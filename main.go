package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"gitea.lan/cubixle/agent/internal"
	"gopkg.in/yaml.v3"
)

var banner = `
   ###     ######   ######## ##    ## ########  #######   #######  
  ## ##   ##    ##  ##       ###   ##    ##    ##     ## ##     ## 
 ##   ##  ##        ##       ####  ##    ##           ##        ## 
##     ## ##   #### ######   ## ## ##    ##     #######   #######  
######### ##    ##  ##       ##  ####    ##    ##        ##        
##     ## ##    ##  ##       ##   ###    ##    ##        ##        
##     ##  ######   ######## ##    ##    ##    ######### #########
`

func main() {
	fmt.Println(banner)
	fmt.Println("------------------------------")

	config, err := loadAgentConfig(".agent22.yml")
	if err != nil {
		log.Fatal(err)
	}

	if config.MaxTries <= 0 {
		config.MaxTries = 3
	}

	if config.JiraMaxResults <= 0 {
		config.JiraMaxResults = 5
	}

	if strings.TrimSpace(config.JiraDoneStatus) == "" {
		config.JiraDoneStatus = strings.TrimSpace(config.JiraPRStatus)
	}

	if strings.TrimSpace(config.JiraDoneStatus) == "" {
		config.JiraDoneStatus = "Done"
	}

	// print out none-sensitive config values for verification
	fmt.Printf("JIRA Base URL: %s\n", config.JiraBaseURL)
	fmt.Printf("JIRA JQL: %s\n", config.JiraJQL)
	fmt.Printf("JIRA Done Status: %s\n", config.JiraDoneStatus)
	fmt.Printf("Max Tries: %d\n", config.MaxTries)

	if err := internal.RunAgent(config); err != nil {
		log.Fatal(err)
	}
}

func loadAgentConfig(path string) (internal.AgentConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return internal.AgentConfig{}, fmt.Errorf("read config file %s: %w", path, err)
	}

	var config internal.AgentConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return internal.AgentConfig{}, fmt.Errorf("parse YAML config %s: %w", path, err)
	}

	config.JiraBaseURL = strings.TrimRight(config.JiraBaseURL, "/")

	return config, nil
}
