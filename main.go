package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"
	//"gopkg.in/yaml.v2"
)

var (
	// FLAGS
	mapLocation   = flag.String("map", os.Getenv("CONFIG_MAP_LOCATION"), "Location of the config map mount.")
	rulesLocation = flag.String("rules", os.Getenv("RULES_LOCATION"), "Filename where the rules file should be written.")
)

func main() {
	flag.Parse()

	log.Printf("Rule Updater loaded.\n")
	log.Printf("ConfigMap location: %s\n", *mapLocation)
	log.Printf("Rules location: %s\n", *rulesLocation)

	// load base config
	updateRules(*mapLocation, *rulesLocation)

	// setup file watcher, will trigger whenever the configmap updates
	watcher, err := WatchFile(*mapLocation, time.Second, func() {
		log.Printf("Configuration files updated.\n")
		updateRules(*mapLocation, *rulesLocation)
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
		log.Fatalf("Generated rules do not pass: %s \n----- \n%s\n", err, rulesToWrite)
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
		return "", fmt.Errorf("Rule rejected: %s.\nReason: %s\n", file, err)
	}
	log.Printf("Rule passed!\n")
	return configManager.Get(), nil
}
