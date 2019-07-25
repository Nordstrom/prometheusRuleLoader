package main //import "github.com/nordstrom/prometheusRuleLoader"

import (
	"bufio"
	"crypto/sha1"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/prometheus/pkg/rulefmt"

	"gopkg.in/matryer/try.v1"
	"gopkg.in/yaml.v2"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

// Controller bye bye error
type Controller struct {
	indexer  cache.Indexer
	queue    workqueue.RateLimitingInterface
	informer cache.Controller
}

type MultiRuleGroups struct {
	Values []rulefmt.RuleGroups
}

var (
	// flags general
	helpFlag            = flag.Bool("help", false, "")
	configmapAnnotation = flag.String("annotation", "nordstrom.net/prometheus2Alerts", "Annotation that states that this configmap contains prometheus rules.")
	rulesLocation       = flag.String("rulespath", "/rules", "Filepath where the rules from the configmap file should be written, this should correspond to a rule_files: location in your prometheus config.")
	reloadEndpoint      = flag.String("endpoint", "http://localhost:9090/-/reload/", "Endpoint of the Prometheus reset endpoint (eg: http://prometheus:9090/-/reload).")
	batchTime           = flag.Int("batchtime", 5, "Time window to batch updates (in seconds, default: 5)")
	// flags - kubeclient
	kubeconfigPath = flag.String("kubeconfig", "", "Path to kubeconfig. Required for out of cluster operation.")
	masterURL      = flag.String("master", "", "The address of the kube api server. Overrides the kubeconfig value, only require for off cluster operation.")

	clientset *kubernetes.Clientset
	lastSha  string
)

const (
	// Resync period for the kube controller loop.
	resyncPeriod = 30 * time.Minute
	// A subdomain added to the user specified domain for all services.
	serviceSubdomain = "svc"
	// A subdomain added to the user specified dmoain for all pods.
	podSubdomain = "pod"
)

func main() {
	flag.Parse()

	if *helpFlag ||
		*configmapAnnotation == "" ||
		*rulesLocation == "" ||
		*reloadEndpoint == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	log.Printf("Rule Updated loaded.\n")
	log.Printf("ConfigMap annotation: %s\n", *configmapAnnotation)
	log.Printf("Rules location: %s\n", *rulesLocation)

	config, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfigPath)
	if err != nil {
		log.Printf("Error building kubeconfig: %s\n", err.Error())
		os.Exit(1)
	}

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Error building client: %s\n", err)
		os.Exit(1)
	}

	log.Println("Building list watcher")
	configmapListWatcher := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "configmaps", v1.NamespaceAll, fields.Everything())
	log.Println("Building queue")
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	indexer, informer := cache.NewIndexerInformer(configmapListWatcher, &v1.ConfigMap{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
	}, cache.Indexers{})

	controller := NewController(queue, indexer, informer)

	indexer.Add(&v1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "myconfigmap",
			Namespace: v1.NamespaceDefault,
		},
	})

	stop := make(chan struct{})
	defer close(stop)
	log.Println("Starting controller...")
	go controller.Run(1, stop)

	select {}
}

// NewController make a new Controller
func NewController(queue workqueue.RateLimitingInterface, indexer cache.Indexer, informer cache.Controller) *Controller {
	return &Controller{
		informer: informer,
		indexer:  indexer,
		queue:    queue,
	}
}

func (c *Controller) processNextItem() bool {
	// get item from queue
	key, quit := c.queue.Get()
	if quit {
		return false
	}

	defer c.queue.Done(key)

	configmapRuleGroups, err := c.buildNewRules()
	if err != nil {
		log.Printf("Unable to build new rules file with error: %s\n", err)
	}

	reloadcheck, err := c.writeFile(configmapRuleGroups)
	if reloadcheck {
		c.tryReloadEndpoint(*reloadEndpoint)
	}
	if err != nil {
		fmt.Println(err)
	}
	time.Sleep(time.Second * time.Duration(*batchTime))
	return true
}

// Run ...
func (c *Controller) Run(threadiness int, stopCh chan struct{}) {
	defer runtime.HandleCrash()

	// Let the workers stop when we are done
	defer c.queue.ShutDown()
	log.Println("Starting Configmap controller")

	go c.informer.Run(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
	log.Println("Stopping Configmap controller")
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *Controller) buildNewRules() ( MultiRuleGroups, error) {

	mapList, err := clientset.CoreV1().ConfigMaps(v1.NamespaceAll).List(meta_v1.ListOptions{})
	if err != nil {
		return MultiRuleGroups{}, err
	}

	ruleGroups, err := c.processConfigMaps(mapList)
	if err != nil {
		fmt.Println(err)
	}

	return ruleGroups, nil
}

func (c *Controller) processConfigMaps(mapList *v1.ConfigMapList) (MultiRuleGroups, error) {
	ruleGroups := MultiRuleGroups{}
	errors := make([]error, 0)

	for _, cm := range mapList.Items {
		anno := cm.GetObjectMeta().GetAnnotations()
		name := cm.GetObjectMeta().GetName()
		namespace := cm.GetObjectMeta().GetNamespace()


		for k := range anno {
			if k == *configmapAnnotation {
				log.Printf("Rule configmap found, processing: %s/%s\n", namespace, name)

				g, err := c.extractValues( fmt.Sprintf("%s-%s", namespace, name), cm.Data )
				if err != nil {
					errors = append(errors, err)
				}


				ruleGroups.Values = append(ruleGroups.Values, g.Values...)

			}
		}
	}
	reterr := assembleErrors(errors)

	return ruleGroups, reterr
}

func (c *Controller) extractValues(fallbackNameStub string, data map[string]string) (MultiRuleGroups, error) {

	// make a bucket for random non fully formed rulegroups (just a single rulegroup) to live
	mrg := MultiRuleGroups{}
	myerrors := make([]error,0)
	for key, value := range data {
		// fallback decoding, first try extracting a RuleGroups, then a RuleGroup, then []Rule
		err, myrulegroups := c.extractRuleGroups(value)
		if err != nil {
			// try rulegroup
			err, myrulegroups := c.extractRuleGroupAsRuleGroups(value)
			if err != nil {
				// try rules array
				err, myrulegroups := c.extractRulesAsRuleGroups(fallbackNameStub, key, value)
				if err != nil {
					myerrors = append(myerrors, fmt.Errorf("Configmap: %s  key: %s does not conform to any of the legal formapts (RuleGroups, RuleGroup or []Rules. Skipping.", fallbackNameStub, key))
				} else {
					myrulegroups, err = c.validateRuleGroups(fallbackNameStub, key, myrulegroups)
					myerrors = append(myerrors,err)
					mrg.Values = append(mrg.Values, myrulegroups)
				}
			} else {
				mrg.Values = append(mrg.Values, myrulegroups)
				myerrors = append(myerrors,err)
			}
		} else {
			mrg.Values = append(mrg.Values, myrulegroups)
			myerrors = append(myerrors,err)
		}
	}

	reterr := assembleErrors(myerrors)


	return mrg, reterr
}

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

func (c *Controller) validateRuleGroups(fallbackname string, keyname string, groups rulefmt.RuleGroups) (rulefmt.RuleGroups, error) {

	// im not using rulegroups.Validate here because i think their current error processing is broken.
	errors := make([]error,0)
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
					myerror := fmt.Errorf("Rule failed validation: configmap:%s, key:%s, groupname: %s, rulename: %s Error: %s", fallbackname, keyname, group.Name, name, err)
					errors = append(errors, myerror)
				}
				c.removeRules(&group, remove)
			}
		}
	}

	reterr := assembleErrors(errors)

	return groups, reterr
}

func (c *Controller) removeRules(group *rulefmt.RuleGroup, list []int) {
	for i := len(list)-1; i >=0; i-- {
		v := list[i]
		group.Rules = append(group.Rules[:v], group.Rules[v+1:]...)
	}
}

func (c *Controller) writeFile(groups MultiRuleGroups) (bool, error) {

	filegroup := rulefmt.RuleGroups{}
	for _, v := range groups.Values {
		filegroup.Groups = append(filegroup.Groups, v.Groups...)
	}

	if len(filegroup.Groups) > 0 {
		rulesyaml, err := yaml.Marshal(filegroup)
		newSha := c.computeSha1(rulesyaml)
		if lastSha != newSha {
			err = c.persistFile(rulesyaml, *rulesLocation)
			if err != nil {
				return false, err
			}
			lastSha = newSha
			return true, nil
		}
		log.Println("No changes, skipping write.")
	}
	log.Println("No rules to write.")
	return false, nil
}

func (c *Controller) persistFile(bytes []byte, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("Unable to open rules file %s for writing. Error: %s", path, err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	byteCount, err := w.WriteString(string(bytes))
	if err != nil {
		return fmt.Errorf("Unable to write generated rules. Error: %s", err)
	}
	log.Printf("Wrote %d bytes.\n", byteCount)
	w.Flush()

	return nil
}

func (c *Controller) tryReloadEndpoint(endpoint string) {
	_ = try.Do(func(attempt int) (bool, error) {
		err := c.reloadEndpoint(*reloadEndpoint)
		if err != nil {
			log.Println(err)
			time.Sleep(10 * time.Second)
			return false, err
		}
		return true, nil
	})
}

func (c *Controller) reloadEndpoint(url string) error {
	client := &http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Unable to reload Prometheus config: %s", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		log.Printf("Prometheus configuration reloaded.")
		return nil
	}

	respBody, _ := ioutil.ReadAll(resp.Body)
	return fmt.Errorf("Unable to reload the Prometheus config. Endpoint: %s, Reponse StatusCode: %d, Response Body: %s", url, resp.StatusCode, string(respBody))
}

func (c *Controller) computeSha1(s []byte) string {
	hash := sha1.New()
	hash.Write(s)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func assembleErrors(myerrors []error) error {
	errorstring := ""
	for _, v := range myerrors {
		errorstring = fmt.Sprintf("%s, %s", errorstring, v)
	}
	if len(errorstring) > 0 {
		return fmt.Errorf(errorstring)
	}
	return nil
}
