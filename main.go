package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	//"gopkg.in/yaml.v2"

	kapi "k8s.io/kubernetes/pkg/api"
	kcache "k8s.io/kubernetes/pkg/client/cache"
	kclient "k8s.io/kubernetes/pkg/client/unversioned"
	kframework "k8s.io/kubernetes/pkg/controller/framework"
	kselector "k8s.io/kubernetes/pkg/fields"
	klabels "k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/util/wait"
)

var (
	// FLAGS
	mapLocation            = flag.String("map", os.Getenv("CONFIG_MAP_LOCATION"), "Location of the config map mount.")
	configmapRulesLocation = flag.String("cmrules", os.Getenv("CM_RULES_LOCATION"), "Filename where the rules from the configmap file should be written.")
	serviceRulesLocation   = flag.String("svrules", os.Getenv("SV_RULES_LOCATION"), "Filename where the rules from the services should be written.")
	reloadEndpoint         = flag.String("endpoint", os.Getenv("PROMETHEUS_RELOAD_ENDPOINT"), "Endpoint of the Prometheus reset endpoint (eg: http://prometheus:9090/-/reload).")

	cluster = flag.Bool("use-kubernetes-cluster-service", true, "If true, use the built in kube cluster for creating the client.")

	helpFlag = flag.Bool("help", false, "")

	lastSvcSha = ""
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

	if *helpFlag {
		flag.PrintDefaults()
		os.Exit(0)
	}

	log.Printf("Rule Updater loaded.\n")
	log.Printf("ConfigMap location: %s\n", *mapLocation)
	log.Printf("ConfigMap Rules location: %s\n", *configmapRulesLocation)
	log.Printf("Service Rules location: %s\n", *serviceRulesLocation)

	// create client
	var kubeClient *kclient.Client
	kubeClient, err := kclient.NewInCluster()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// load base config
	updateConfigMapRules(*mapLocation, *configmapRulesLocation)
	updateServiceRules(kubeClient, *serviceRulesLocation)

	// setup file watcher, will trigger whenever the configmap updates
	watcher, err := WatchFile(*mapLocation, time.Second, func() {
		log.Printf("ConfigMap files updated.\n")
		updateConfigMapRules(*mapLocation, *configmapRulesLocation)
		reloadRules(*reloadEndpoint)
	})
	if err != nil {
		log.Fatalf("Unable to watch ConfigMap: %s\n", err)
	}

	// setup watcher for services
	_ = watchForServices(kubeClient, func(interface{}) {
		log.Printf("Services have updated.\n")
		check := updateServiceRules(kubeClient, *serviceRulesLocation)
		if check {
			reloadRules(*reloadEndpoint)
		}
	})

	defer func() {
		log.Printf("Cleaning up.")
		watcher.Close()
	}()

	select {}
}

func loadConfig(configFile string) string {
	configData, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Cannot read ConfigMap file: %s\n", err)
	}

	return string(configData)
}

func updateConfigMapRules(mapLocation string, rulesLocation string) {
	log.Println("Processing ConfigMap rules.")
	fileList := GatherFilesFromConfigmap(mapLocation)

	var rulesToWrite string

	for _, file := range fileList {
		content, err := processRuleFile(file)
		if err != nil {
			log.Printf("%s", err)
		} else {
			rulesToWrite += fmt.Sprintf("%s\n", content)
		}
	}

	err := CheckRules(rulesToWrite)
	if err != nil {
		log.Printf("Generated ConfigMap rules do not pass: %s.\n%s\n", err, rulesToWrite)
	}

	err = writeRules(rulesToWrite, rulesLocation)
	if err != nil {
		log.Printf("%s\n", err)
	}
}

func updateServiceRules(kubeClient *kclient.Client, rulesLocation string) bool {
	log.Println("Processing Service rules.")

	ruleList := GatherRulesFromServices(kubeClient)

	var rulesToWrite string
	for _, rule := range ruleList {
		content, err := processRuleString(rule, "Service")
		if err != nil {
			log.Printf("%s", err)
		} else {
			rulesToWrite += fmt.Sprintf("%s\n", content)
		}
	}

	err := CheckRules(rulesToWrite)
	if err != nil {
		log.Printf("Generated Service rules do not pass: %s.\n%s\n", err, rulesToWrite)
	}

	// only write and
	newSha := computeSha1(rulesToWrite)
	if lastSvcSha != newSha {
		err = writeRules(rulesToWrite, rulesLocation)
		if err != nil {
			log.Printf("%s\n", err)
		}
		lastSvcSha = newSha
		return true
	} else {
		log.Println("No changes, skipping write.")
	}
	return false
}

func writeRules(rules string, rulesLocation string) error {
	f, err := os.Create(rulesLocation)
	if err != nil {
		return fmt.Errorf("Unable to open rules file %s for writing. Error: %s", rulesLocation, err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	byteCount, err := w.WriteString(rules)
	if err != nil {
		return fmt.Errorf("Unable to write generated rules. Error: %s", err)
	}
	log.Printf("Wrote %d bytes.\n", byteCount)
	w.Flush()

	return nil
}

func processRuleFile(file string) (string, error) {
	configManager := NewMutexConfigManager(loadConfig(file))
	defer func() {
		configManager.Close()
	}()

	rule := configManager.Get()
	_, err := processRuleString(rule, fmt.Sprintf("Configmap rule: %s", file))
	if err != nil {
		return "", err
	}

	return rule, nil
}

func processRuleString(rule string, metadata string) (string, error) {
	log.Printf("Processing rule: %s", metadata)

	err := CheckRules(rule)
	if err != nil {
		return "", fmt.Errorf("Rule rejected: %s. Reason: %s\n", metadata, err)
	}
	log.Printf("Rule passed!\n")

	return rule, nil
}

func reloadRules(url string) {
	client := &http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Unable to reload Prometheus config: %s\n", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		log.Printf("Prometheus configuration reloaded.")
	} else {
		respBody, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Unable to reload the Prometheus config. Endpoint: %s, Reponse StatusCode: %d, Response Body: %s", url, resp.StatusCode, string(respBody))
	}
}

func GatherFilesFromConfigmap(mapLocation string) []string {
	fileList := []string{}
	err := filepath.Walk(mapLocation, func(path string, f os.FileInfo, err error) error {
		stat, err := os.Stat(path)
		if err != nil {
			log.Printf("Cannot stat %s, %s\n", path, err)
		}
		if !stat.IsDir() {
			// ignore the configmap /..dirname directories
			if !(strings.Contains(path, "/..")) {
				fileList = append(fileList, path)
			}
		}
		return nil
	})
	if err != nil {
		// not sure what I might see here, so making this fatal for now
		log.Printf("Cannot process path: %s, %s\n", mapLocation, err)
	}
	return fileList
}

func GatherRulesFromServices(kubeClient *kclient.Client) []string {
	si := kubeClient.Services(kapi.NamespaceAll)
	serviceList, err := si.List(kapi.ListOptions{
		LabelSelector: klabels.Everything(),
		FieldSelector: kselector.Everything()})
	if err != nil {
		log.Printf("Unable to list services: %s", err)
	}

	ruleList := []string{}

	for _, svc := range serviceList.Items {
		anno := svc.GetObjectMeta().GetAnnotations()
		name := svc.GetObjectMeta().GetName()
		log.Printf("Processing Service - %s\n", name)

		for k, v := range anno {
			log.Printf("- %s", k)
			if k == "nordstrom.net/alerts" {
				var alerts interface{}
				err := json.Unmarshal([]byte(v), &alerts)
				if err != nil {
					log.Printf("Error decoding json object that contains alert(s): %s\n", err)
				}
				if reflect.TypeOf(alerts).Kind() == reflect.Slice {
					collection := reflect.ValueOf(alerts)
					for i := 0; i < collection.Len(); i++ {
						ruleList = append(ruleList, collection.Index(i).String())
					}
				}
				if reflect.TypeOf(alerts).Kind() == reflect.String {
					ruleList = append(ruleList, reflect.ValueOf(alerts).String())
				}

			}

		}

	}
	return ruleList
}

func createServiceLW(kubeClient *kclient.Client) *kcache.ListWatch {
	return kcache.NewListWatchFromClient(kubeClient, "services", kapi.NamespaceAll, kselector.Everything())
}

func watchForServices(kubeClient *kclient.Client, callback func(interface{})) kcache.Store {
	serviceStore, serviceController := kframework.NewInformer(
		createServiceLW(kubeClient),
		&kapi.Service{},
		0,
		kframework.ResourceEventHandlerFuncs{
			AddFunc:    callback,
			DeleteFunc: callback,
			UpdateFunc: func(a interface{}, b interface{}) { callback(b) },
		},
	)
	go serviceController.Run(wait.NeverStop)
	return serviceStore
}

func computeSha1(payload string) string {
	hash := sha1.New()
	hash.Write([]byte(payload))

	return fmt.Sprintf("%x", hash.Sum(nil))
}
