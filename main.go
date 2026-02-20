package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"gitea.lan/cubixle/agent/internal"
	"gopkg.in/yaml.v3"
)

var banner = `
-------------------------------
░█▀▀▄░█▀▀▀░█▀▀░█▀▀▄░▀█▀░█▀█░█▀█
▒█▄▄█░█░▀▄░█▀▀░█░▒█░░█░░▒▄▀░▒▄▀
▒█░▒█░▀▀▀▀░▀▀▀░▀░░▀░░▀░░█▄▄░█▄▄
-------------------------------
Created by Cubixle
`

func main() {
	pullRequestMode := flag.Bool("pull-request-mode", false, "monitor Gitea pull request comments and apply changes via opencode")

	flag.Parse()

	fmt.Println(banner)

	config, err := loadAgentConfig(".agent22.yml")
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	applyConfigDefaults(&config)

	var runErr error
	if *pullRequestMode {
		runErr = internal.RunPullRequestMode(config)
	} else {
		runErr = internal.RunAgent(config)
	}

	if runErr != nil {
		slog.Error("run agent", "error", runErr)
		os.Exit(1)
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

	return config, nil
}

func applyConfigDefaults(config *internal.AgentConfig) {
	if config.WaitTimeSeconds <= 0 {
		config.WaitTimeSeconds = 30
	}
}
