package main

import (
	"fmt"
	"github.com/nordstrom/kubernetes/pkg/api/v1"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/kubernetes/scheme"
	"time"

	"github.com/prometheus/prometheus/pkg/rulefmt"

	corev1 "k8s.io/api/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)


const (
	controllerAgentName = "prometheus-rule-loader-controller"

	// SuccessSynced is used as part of the Event 'reason' when a Foo is synced
	SuccessSynced = "Synced"
	// ErrResourceExists is used as part of the Event 'reason' when a Foo fails
	// to sync due to a Deployment of the same name already existing.
	ErrResourceExists = "ErrResourceExists"

	// MessageResourceExists is the message used for Events when a resource
	// fails to sync due to a Deployment already existing
	MessageResourceExists = "Resource %q already exists and is not managed by Foo"
	// MessageResourceSynced is the message used for an Event fired when a Foo
	// is synced successfully
	MessageResourceSynced = "Foo synced successfully"

	ErrInvalidKey = "InvalidKey"
	ValidKey = "ValidKey"
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

	resourceVersionMap   map[string]string
	interestingAnnotation string
	reloadEndpoint       string
}


type MultiRuleGroups struct {
	Values []rulefmt.RuleGroups
}


func NewController(
	kubeclientset *kubernetes.Clientset,
	configmapInformer corev1informers.ConfigMapInformer,
	interestingAnnotation string,
	reloadEndpoint string,
	) *Controller {

		utilruntime.Must(scheme.AddToScheme(scheme.Scheme))
		klog.Infof("Setting up event handlers")
		eventBroadcaster := record.NewBroadcaster()
		eventBroadcaster.StartLogging(klog.Infof)
		eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
		recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

		controller := &Controller{
			kubeclientset:    kubeclientset,
			configmapsLister: configmapInformer.Lister(),
			configmapsSynced: configmapInformer.Informer().HasSynced,
			workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "configmaps"),
			recorder:         recorder,
			interestingAnnotation: interestingAnnotation,
			reloadEndpoint: reloadEndpoint,
		}

		klog.Info("Setting up event handlers")
		configmapInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: controller.handleConfigMap,
			UpdateFunc: func(old, new interface{}) {
				newCM := new.(*corev1.ConfigMap)
				oldCM := old.(*corev1.ConfigMap)
				if newCM.ResourceVersion == oldCM.ResourceVersion {
					return
				}
				controller.handleConfigMap(newCM)
			},
			DeleteFunc: controller.handleConfigMap,
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
// converge the two. It then updates the Status block of the Foo resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	//// Convert the namespace/name string into a distinct namespace and name
	//namespace, name, err := cache.SplitMetaNamespaceKey(key)
	//if err != nil {
	//	utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
	//	return nil
	//}
	//
	//// Get the CM resource with this namespace/name
	//configmap, err := c.configmapsLister.ConfigMaps(namespace).Get(name)
	//if err != nil {
	//	// the deployment may have already been deleted
	//	if errors.IsNotFound(err) {
	//		utilruntime.HandleError(fmt.Errorf("configmap '%s' in work queue no longer exists", key))
	//		return nil
	//	}
	//
	//	return err
	//}
	//
	//
	//
	//
	//// Finally, we update the status block of the Foo resource to reflect the
	//// current state of the world
	//err = c.updateConfigMapStatus(configmap)
	//if err != nil {
	//	return err
	//}
	//
	//// TODO we might want to do this later
	//c.recorder.Event(configmap, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

func (c *Controller) handleConfigMap(cm *corev1.ConfigMap) {
	// current nieve implimentation
	// 1. Something changed and we have arrived here
	// 2. Get all configmaps clusterwide filter on annotation
	// 3. Iterate through each pulling out and validating the rules building a rulesgroups if nessecary
	// 4. Concat rules groups
	// 5. Verify unique naming to rules groups
	// 6. verify hash against last hash
	// 7. if different write file and reload prometheus

	// potentially better implimentation
	// 1. something changed...
	// 2. Get all configmaps clusterwide filter on annotation
	// 3. get RV for each hit version and create a little fast lookup table `namespace/name`:`resource version`
	// 4. check the list and see if there are any changes, if so build rules etc

	if c.isRuleConfigMap(cm) {
		mapList, err := c.kubeclientset.CoreV1().ConfigMaps(v1.NamespaceAll).List(metav1.ListOptions{})
		if err != nil {
			klog.Errorf("Unable to collect configmaps from the cluster; %s", err)
			return
		}
		if c.haveConfigMapsChanged(mapList) {
			finalConfig := c.buildFinalConfig(mapList)
		}
	}
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
		// fallback decoding, first try extracting a RuleGroups, then a RuleGroup, then []Rule
		err, myrulegroups := c.extractRuleGroups(value)
		if err != nil {
			// try rulegroup
			err, myrulegroups := c.extractRuleGroupAsRuleGroups(value)
			if err != nil {
				// try rules array
				err, myrulegroups := c.extractRulesAsRuleGroups(fallbackNameStub, key, value)
				if err != nil {
					errorMsg := fmt.Sprintf("Configmap: %s key: %s does not conform to any of the legal formats (RuleGroups, RuleGroup or []Rules. Skipping.", fallbackNameStub, key)
					c.recordWarningOnConfigMap(cm, ErrInvalidKey, errorMsg)

				} else {
					myrulegroups := c.validateRuleGroups(cm, key, myrulegroups)
					mrg.Values = append(mrg.Values, myrulegroups)
					successMessage := fmt.Sprintf("Configmap: %s key: %s Accepted.", fallbackNameStub, key)
					c.recordWarningOnConfigMap(cm, ValidKey, successMessage)
				}
			} else {
				mrg.Values = append(mrg.Values, myrulegroups)
				successMessage := fmt.Sprintf("Configmap: %s key: %s Accepted.", fallbackNameStub, key)
				c.recordWarningOnConfigMap(cm, ValidKey, successMessage)
			}
		} else {
			mrg.Values = append(mrg.Values, myrulegroups)

			successMessage := fmt.Sprintf("Configmap: %s key: %s Accepted.", fallbackNameStub, key)
			c.recordWarningOnConfigMap(cm, ValidKey, successMessage)
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
	for _, group := range groups.Groups {

		for i, r := range group.Rules {
			remove := make([]int,0)
			for _, err := range r.Validate() {
				if err != nil {
					remove = append(remove, i)
					name := r.Alert
					if name == "" {
						name = r.Record
					}
					errorMsg := fmt.Sprintf("Rule failed validation: configmap:%s, key:%s, groupname: %s, rulename: %s Error: %s", nameStub, keyname, group.Name, name, err)
					c.recorder.Event(cm, corev1.EventTypeWarning, ErrInvalidKey, errorMsg )
					klog.Warning(errorMsg)
				}
				c.removeRules(&group, remove)
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
	annotations := cm.GetObjectMeta().GetAnnotations()

	for key := range annotations {
		if key == c.interestingAnnotation {
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

func (c *Controller) recordWarningOnConfigMap(cm *corev1.ConfigMap, reason, msg string) {
	c.recorder.Event(cm, corev1.EventTypeWarning, ErrInvalidKey, msg )
	klog.Warning(msg)
}

func (c *Controller) recordSuccessOnConfigMap(cm *corev1.ConfigMap, reason, msg string) {
	c.recorder.Event(cm, corev1.EventTypeNormal, ErrInvalidKey, msg )
	klog.Warning(msg)
}

func (c *Controller) createNameStub(cm *corev1.ConfigMap) string {
	name := cm.GetObjectMeta().GetName()
	namespace := cm.GetObjectMeta().GetNamespace()

	return fmt.Sprintf("%s-%s", namespace, name)
}

func (c *Controller) saltRuleGroupNames(rgs *rulefmt.RuleGroups) *rulefmt.RuleGroups {
	usedNamed := make(map[string]string)
	for _, g := range rgs.Groups {

	}
}