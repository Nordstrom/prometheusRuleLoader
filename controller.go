package main

import (
	"bufio"
	"fmt"
	"gopkg.in/matryer/try.v1"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/prometheus/pkg/rulefmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)


const (
	controllerAgentName = "prometheus-rule-loader-controller"

	ErrInvalidKey = "InvalidKey"
	ValidKey = "ValidKey"
	letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

// Controller is the controller implementation for Foo resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset        kubernetes.Interface

	configmapsLister     corev1listers.ConfigMapLister
	configmapsSynced     cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue            workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder             record.EventRecorder

	resourceVersionMap         map[string]string
	interestingAnnotation      *string
	reloadEndpoint             *string
	rulesPath                  *string
	randSrc                    *rand.Source
	configmapEventRecorderFunc func(cm *corev1.ConfigMap, eventtype,reason, msg string)
}


type MultiRuleGroups struct {
	Values []rulefmt.RuleGroups
}


func NewController(
	kubeclientset *kubernetes.Clientset,
	configmapInformer corev1informers.ConfigMapInformer,
	interestingAnnotation *string,
	reloadEndpoint *string,
	rulesPath *string,
	) *Controller {

		utilruntime.Must(scheme.AddToScheme(scheme.Scheme))
		klog.Infof("Setting up event handlers")
		eventBroadcaster := record.NewBroadcaster()
		eventBroadcaster.StartLogging(klog.Infof)
		eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
		recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

		rsource := rand.NewSource(time.Now().UnixNano())

		controller := &Controller{
			kubeclientset:         kubeclientset,
			configmapsLister:      configmapInformer.Lister(),
			configmapsSynced:      configmapInformer.Informer().HasSynced,
			workqueue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "configmaps"),
			recorder:              recorder,
			interestingAnnotation: interestingAnnotation,
			reloadEndpoint:        reloadEndpoint,
			rulesPath:             rulesPath,
			randSrc:               &rsource,
			resourceVersionMap:    make(map[string]string),
		}

		// is this idomatic?
		controller.configmapEventRecorderFunc = controller.recordEventOnConfigMap

		klog.Info("Setting up event handlers")
		configmapInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueConfigMap,
			UpdateFunc: func(old, new interface{}) {
				newCM := new.(*corev1.ConfigMap)
				oldCM := old.(*corev1.ConfigMap)
				if newCM.ResourceVersion == oldCM.ResourceVersion {
					return
				}
				controller.enqueueConfigMap(newCM)
			},
			DeleteFunc: controller.enqueueConfigMap,
		})

		return controller
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Infof("Starting %s", controllerAgentName)

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.configmapsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the cm resource
// with the current status of the resource.
//
// Only return errors that are transient, a return w/ an error creates a rate
// limited requeue of the resource.
func (c *Controller) syncHandler(key string) error {
	//// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the CM resource with this namespace/name
	configmap, err := c.configmapsLister.ConfigMaps(namespace).Get(name)
	if err != nil {
		// the cm may have already been deleted
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("configmap '%s' in work queue no longer exists, rebuilding rules config", key))
		} else {
			return err
		}
	}


	// current implimentation
	// 1. some configmap changed...
	// 1b. If it was nil (deleted) we have no choice but to rebuild skip to 2d1.
	// 2. does configmap have annotation
	// 2b. Get all configmaps clusterwide filter on annotation
	// 2c. Check each cm resource version against a lookup table
	// 2d. if there are any misses
	// 2d1. rebuild config

	// I don't love this bypass
	bypassCheck := false

	if configmap == nil {
		// deleted
		bypassCheck = true
	}

	if c.isRuleConfigMap(configmap) || bypassCheck {
		mapList, err := c.kubeclientset.CoreV1().ConfigMaps(corev1.NamespaceAll).List(metav1.ListOptions{})
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("Unable to collect configmaps from the cluster; %s", err))
			return nil
		}

		if c.haveConfigMapsChanged(mapList) || bypassCheck {
			finalrules := c.buildFinalConfig(mapList)
			if err != nil {
				utilruntime.HandleError(err)
				return nil
			}

			// write
			err = c.persistRulesGroup(finalrules)
			if err != nil {
				utilruntime.HandleError(err)
			}

			// reload
			c.tryConfigReload()

		}

	}

	return nil
}


// get the cm on the workqueue
func (c *Controller) enqueueConfigMap(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) buildFinalConfig(mapList *corev1.ConfigMapList) *rulefmt.RuleGroups {
	finalRules := MultiRuleGroups{}

	for _, cm := range mapList.Items {
		if c.isRuleConfigMap(&cm) {
			cmRules := c.extractValues(&cm)
			if len(cmRules.Values) > 0 {
				finalRules.Values = append(finalRules.Values, cmRules.Values...)

			}
		}
	}

	finalRGs := c.decomposeMultiRuleGroupIntoRuleGroups(&finalRules)
	return c.saltRuleGroupNames(finalRGs)
}


func (c *Controller) extractValues(cm *corev1.ConfigMap) (MultiRuleGroups) {

	fallbackNameStub := c.createNameStub(cm)

	// make a bucket for random non fully formed rulegroups (just a single rulegroup) to live
	mrg := MultiRuleGroups{}

	for key, value := range cm.Data {
		// try each encoding
		// try to extract a rulegroups
		var rulegroups rulefmt.RuleGroups
		var err error
		err, rulegroups = c.extractRuleGroups(value)
		if err != nil {
			// try to extract a rulegroup as a rulegroups
			err, rulegroups = c.extractRuleGroupAsRuleGroups(value)
			if err != nil {
				// try to extract a rules array as a rulegroups
				_, rulegroups = c.extractRulesAsRuleGroups(fallbackNameStub, key, value)
			}
		}

		if len(rulegroups.Groups) == 0 {
			errorMsg := fmt.Sprintf("Configmap: %s key: %s does not conform to any of the legal formats (RuleGroups, RuleGroup or []Rules. Skipping.", fallbackNameStub, key)
			c.configmapEventRecorderFunc(cm, corev1.EventTypeWarning, ErrInvalidKey, errorMsg)
		} else {
			// validate the rules
			rulegroups = c.validateRuleGroups(cm, key, rulegroups)

			//if there are groups and rules
			totalrules := c.countRuleGroupsRules(rulegroups)
			if len(rulegroups.Groups) > 0 && totalrules > 0 {
				// append
				mrg.Values = append(mrg.Values, rulegroups)
				successMessage := fmt.Sprintf("Configmap: %s key: %s Accepted with %d rulegroups and %d total rules.", fallbackNameStub, key, len(rulegroups.Groups), totalrules)
				c.configmapEventRecorderFunc(cm, corev1.EventTypeNormal, ValidKey, successMessage)
			} else {
				failMessage := fmt.Sprintf("Configmap: %s key: %s Rejected, no valid rules.", fallbackNameStub, key)
				c.configmapEventRecorderFunc(cm, corev1.EventTypeWarning, ErrInvalidKey, failMessage)
			}
		}

	}

	return mrg
}


// for reference
// from prometheus/pkg/rulefmt/rulefmt.go
//type RuleGroups struct {
//	Groups []RuleGroup `yaml:"groups"`
//}
func (c *Controller) extractRuleGroups(value string) (error, rulefmt.RuleGroups) {
	groups := rulefmt.RuleGroups{}
	err := yaml.Unmarshal([]byte(value), &groups)
	if err != nil {
		return err, rulefmt.RuleGroups{}
	}
	if len(groups.Groups) == 0 {
		return fmt.Errorf("No RuleGroups"), groups
	}

	return nil, groups
}

// for reference
// from prometheus/pkg/rulefmt/rulefmt.go
//type RuleGroup struct {
//	Name     string         `yaml:"name"`
//	Interval model.Duration `yaml:"interval,omitempty"`
//	Rules    []Rule         `yaml:"rules"`
//}
func (c *Controller) extractRuleGroupAsRuleGroups(value string) (error, rulefmt.RuleGroups) {
	group := rulefmt.RuleGroup{}
	err := yaml.Unmarshal([]byte(value), &group)
	if err != nil {
		return err, rulefmt.RuleGroups{}
	}
	if len(group.Rules) == 0 {
		return fmt.Errorf("No RuleGroup"), rulefmt.RuleGroups{}
	}

	wrapper := rulefmt.RuleGroups{}
	wrapper.Groups = append(wrapper.Groups, group)

	return nil, wrapper
}

// []Rule
// for reference
// from prometheus/pkg/rulefmt/rulefmt.go
//type Rule struct {
//	Record      string            `yaml:"record,omitempty"`
//	Alert       string            `yaml:"alert,omitempty"`
//	Expr        string            `yaml:"expr"`
//	For         model.Duration    `yaml:"for,omitempty"`
//	Labels      map[string]string `yaml:"labels,omitempty"`
//	Annotations map[string]string `yaml:"annotations,omitempty"`
//}
func (c *Controller) extractRulesAsRuleGroups(fallbackName string, key string, value string) (error, rulefmt.RuleGroups){
	rules := make([]rulefmt.Rule,0)
	err := yaml.Unmarshal([]byte(value), &rules)
	if err != nil {
		return err, rulefmt.RuleGroups{}
	}
	if len(rules) == 0 {
		return fmt.Errorf("No rules"), rulefmt.RuleGroups{}
	}

	rgName := fmt.Sprintf("%s-%s", fallbackName, key)
	rg := rulefmt.RuleGroup{}
	rg.Name = rgName
	rg.Rules = rules

	wrapper := rulefmt.RuleGroups{}
	wrapper.Groups = append(wrapper.Groups, rg)

	return nil, wrapper
}


func (c *Controller) validateRuleGroups(cm *corev1.ConfigMap, keyname string, groups rulefmt.RuleGroups) (rulefmt.RuleGroups) {
	nameStub := c.createNameStub(cm)
	// im not using rulegroups.Validate here because i think their current error processing is broken.
	for i := 0; i < len(groups.Groups); i++ {

		for j := 0; j < len(groups.Groups[i].Rules); j++ {
			remove := make([]int,0)
			r := groups.Groups[i].Rules[j]

			// Validate of any particular rule can return multiple errors
			for _, err := range r.Validate() {
				if err != nil {
					remove = append(remove, i)
					name := r.Alert

					// recording rules have no names so therefore we'll use the value of "Record" in the error
					if name == "" {
						name = r.Record
					}
					errorMsg := fmt.Sprintf("Rule failed validation: Namespace-ConfigMap:%s, Key:%s, GroupName: %s, Rule Name/Record: %s Error: %s", nameStub, keyname, groups.Groups[i].Name, name, err)
					c.configmapEventRecorderFunc(cm, corev1.EventTypeWarning, ErrInvalidKey, errorMsg)
				}
				c.removeRules(&groups.Groups[i], remove)
			}
		}
	}

	return groups
}

func (c *Controller) removeRules(group *rulefmt.RuleGroup, list []int) {
	for i := len(list)-1; i >=0; i-- {
		v := list[i]
		group.Rules = append(group.Rules[:v], group.Rules[v+1:]...)
	}
}

func (c *Controller) isRuleConfigMap(cm *corev1.ConfigMap) bool {
	if cm == nil {
		return false
	}
	annotations := cm.GetObjectMeta().GetAnnotations()

	for key := range annotations {
		if key == *c.interestingAnnotation {
			return true
		}
	}

	return false
}

func (c *Controller) haveConfigMapsChanged(mapList *corev1.ConfigMapList) bool {
	changes := false
	for _, cm := range mapList.Items {
		if c.isRuleConfigMap(&cm) {
			stub := c.createNameStub(&cm)
			val, ok := c.resourceVersionMap[stub];
			if !ok {
				// new configmap
				changes = true
			}
			if cm.ResourceVersion != val {
				// changed configmap
				changes = true
			}
			c.resourceVersionMap[stub] = cm.ResourceVersion
		}
	}

	return changes
}

func (c *Controller) decomposeMultiRuleGroupIntoRuleGroups(mrg *MultiRuleGroups) *rulefmt.RuleGroups {
	finalRuleGroup := rulefmt.RuleGroups{}
	for _, rg := range mrg.Values {
		finalRuleGroup.Groups = append(finalRuleGroup.Groups, rg.Groups...)
	}
	return &finalRuleGroup
}

func (c *Controller) recordEventOnConfigMap(cm *corev1.ConfigMap, eventtype, reason, msg string) {
	c.recorder.Event(cm, eventtype, reason, msg )
	if eventtype == corev1.EventTypeWarning {
		klog.Warning(msg)
	}
}

func (c *Controller) createNameStub(cm *corev1.ConfigMap) string {
	name := cm.GetObjectMeta().GetName()
	namespace := cm.GetObjectMeta().GetNamespace()

	return fmt.Sprintf("%s-%s", namespace, name)
}

func (c *Controller) saltRuleGroupNames(rgs *rulefmt.RuleGroups) *rulefmt.RuleGroups {
	usedNames := make(map[string]string)
	for i:=0; i < len(rgs.Groups); i++ {
		if _, ok := usedNames[rgs.Groups[i].Name]; ok {
			// used name, salt
			rgs.Groups[i].Name = fmt.Sprintf("%s-%s", rgs.Groups[i].Name, c.generateRandomString(5))
		}
		usedNames[rgs.Groups[i].Name] = "yes"
	}
	return rgs
}

func (c *Controller) persistRulesGroup(rulesGroup *rulefmt.RuleGroups) error {

	rulesBytes, err := yaml.Marshal(*rulesGroup)
	if err != nil {
		return err
	}


	f, err := os.Create(*c.rulesPath)
	if err != nil {
		return fmt.Errorf("Unable to open rules file %s for writing. Error: %s", *c.rulesPath, err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	byteCount, err := w.WriteString(string(rulesBytes))
	if err != nil {
		return fmt.Errorf("Unable to write generated rules. Error: %s", err)
	}
	klog.Infof("Wrote %d bytes.\n", byteCount)
	w.Flush()

	return nil
}

func (c *Controller) tryConfigReload() {
	_ = try.Do(func(attempt int) (bool, error) {
		err := c.configReload(*c.reloadEndpoint)
		if err != nil {
			klog.Error(err)
			time.Sleep(10 * time.Second)
			return false, err
		}
		return true, nil
	})
}

func (c *Controller) configReload(url string) error {
	client := &http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Unable to reload Prometheus config: %s", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		klog.Info("Prometheus configuration reloaded.")
		return nil
	}

	respBody, _ := ioutil.ReadAll(resp.Body)
	return fmt.Errorf("Unable to reload the Prometheus config. Endpoint: %s, Reponse StatusCode: %d, Response Body: %s", url, resp.StatusCode, string(respBody))
}

func (c *Controller) countRuleGroupsRules(rgs rulefmt.RuleGroups) int {
	count := 0
	for _, rg := range rgs.Groups {
		count += len(rg.Rules)
	}
	return count
}

// borrowed from here https://stackoverflow.com/questions/22892120/how-to-generate-a-random-string-of-a-fixed-length-in-go
func (c *Controller) generateRandomString(n int) string {
	b := make([]byte, n)
	src := *c.randSrc
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}