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

// Rule ...
type Rule struct {
	Record      string            `yaml:"record,omitempty"`
	Alert       string            `yaml:"alert,omitempty"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for,omitempty"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`

	//catchall
	XXX map[string]interface{} `yaml:",inline"`
}

// RuleFile ...
type RuleFile struct {
	Groups []RuleGroup `yaml:"groups"`

	//catchall
	XXX map[string]interface{} `yaml:",inline"`
}

// RuleGroup ...
type RuleGroup struct {
	Name     string        `yaml:"name"`
	Rules    []Rule        `yaml:"rules"`
	Interval time.Duration `yaml:"interval,omitempty"`

	//catchall
	XXX map[string]interface{} `yaml:",inline"`
}

// Controller bye bye error
type Controller struct {
	indexer  cache.Indexer
	queue    workqueue.RateLimitingInterface
	informer cache.Controller
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
	lastSha   string
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
	}

	configmapListWatcher := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "configmaps", v1.NamespaceAll, fields.Everything())
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

	indexer.Add(&v1.Pod{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "myconfigmap",
			Namespace: v1.NamespaceDefault,
		},
	})

	stop := make(chan struct{})
	defer close(stop)
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

	myRuleFile := RuleFile{}

	g, err := c.buildNewRules(key.(string))
	if err != nil {
		log.Printf("Unable to build new rules file with error: %s\n", err)
	}
	myRuleFile.Groups = g
	reloadcheck, err := c.writeFile(&myRuleFile)
	if reloadcheck {
		c.tryReloadEndpoint(*reloadEndpoint)
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

func (c *Controller) buildNewRules(key string) ([]RuleGroup, error) {
	groups := []RuleGroup{}

	mapList, err := clientset.CoreV1().ConfigMaps(v1.NamespaceAll).List(meta_v1.ListOptions{})
	if err != nil {
		return groups, err
	}

	c.processConfigMaps(mapList, &groups)

	return groups, nil
}

func (c *Controller) processConfigMaps(mapList *v1.ConfigMapList, groups *[]RuleGroup) {
	for _, cm := range mapList.Items {
		om := cm.GetObjectMeta()
		anno := om.GetAnnotations()
		name := om.GetName()
		namespace := om.GetNamespace()

		for k := range anno {
			if k == *configmapAnnotation {
				log.Printf("Rule configmap found, processing: %s/%s\n", namespace, name)
				for cmk, cmv := range cm.Data {
					err := c.extractValues(cmk, cmv, groups)
					if err != nil {
						log.Printf("Rule %s in configmap %s/%s does not conform to format, skipping. Error: %s\n", cmk, namespace, name, err)
					}
				}
			}
		}
	}
}

func (c *Controller) extractValues(key string, value string, groupPtr *[]RuleGroup) error {
	rg := RuleGroup{}
	rg.Name = key
	err := yaml.Unmarshal([]byte(value), &rg.Rules)
	if err != nil {
		fmt.Printf("%s\n", err)
		return err
	}
	*groupPtr = append(*groupPtr, rg)
	return nil
}

func (c *Controller) writeFile(rules *RuleFile) (bool, error) {

	if len(rules.Groups) > 0 {
		rulesyaml, err := yaml.Marshal(rules)
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
