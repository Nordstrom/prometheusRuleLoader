package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
	//"gopkg.in/yaml.v2"
)

var (
	// FLAGS
	mapLocation    = flag.String("map", os.Getenv("CONFIG_MAP_LOCATION"), "Location of the config map mount.")
	rulesLocation  = flag.String("rules", os.Getenv("RULES_LOCATION"), "Filename where the rules file should be written.")
	reloadEndpoint = flag.String("endpoint", os.Getenv("PROMETHEUS_RELOAD_ENDPOINT"), "Endpoint of the Prometheus reset endpoint (eg: http://prometheus:9090/-/reload).")

	helpFlag = flag.Bool("help", true, "")
)

func main() {
	flag.Parse()

	if *helpFlag {
		flag.PrintDefaults()
		os.Exit(0)
	}

	log.Printf("Rule Updater loaded.\n")
	log.Printf("ConfigMap location: %s\n", *mapLocation)
	log.Printf("Rules location: %s\n", *rulesLocation)

	// load base config
	updateRules(*mapLocation, *rulesLocation)
	reloadRules(*reloadEndpoint)
	// setup file watcher, will trigger whenever the configmap updates
	watcher, err := WatchFile(*mapLocation, time.Second, func() {
		log.Printf("Configuration files updated.\n")
		updateRules(*mapLocation, *rulesLocation)
		reloadRules(*reloadEndpoint)
	})
	if err != nil {
		log.Fatalf("Unable to watch ConfigMap: %s\n", err)
	}

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

func updateRules(mapLocation string, rulesLocation string) {
	log.Println("Processing rules.")

	fileList := []string{}
	err := filepath.Walk(mapLocation, func(path string, f os.FileInfo, err error) error {
		stat, err := os.Stat(path)
		if err != nil {
			log.Printf("Cannot stat %s, %s\n", path, err)
		}
		if !stat.IsDir() {
			fileList = append(fileList, path)
		}
		return nil
	})
	if err != nil {
		// not sure what I might see here, so making this fatal for now
		log.Fatalf("Cannot process path: %s, %s\n", mapLocation, err)
	}

	var rulesToWrite string

	for _, file := range fileList {
		content, err := processRule(file)
		if err != nil {
			log.Printf("%s", err)
		} else {
			rulesToWrite += content
		}
	}

	err = CheckRules(rulesToWrite)
	if err != nil {
		log.Fatalf("Generated rules do not pass: %s.\n%s\n", err, rulesToWrite)
	}

	f, err := os.Create(rulesLocation)
	if err != nil {
		log.Printf("Unable to open rules file %s for writing. Error: %s", rulesLocation, err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	byteCount, err := w.WriteString(rulesToWrite)
	if err != nil {
		log.Printf("Unable to write generated rules. Error: %s", err)
	}
	log.Printf("Wrote %d bytes.\n", byteCount)
	w.Flush()

}

func processRule(file string) (string, error) {
	log.Printf("Processing rule: %s\n", file)
	configManager := NewMutexConfigManager(loadConfig(file))
	defer func() {
		configManager.Close()
	}()

	err := CheckRules(configManager.Get())
	if err != nil {
		return "", fmt.Errorf("Rule rejected: %s. Reason: %s\n", file, err)
	}
	log.Printf("Rule passed!\n")
	return configManager.Get(), nil
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
