package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gitea.lan/cubixle/agent/internal"
	"gopkg.in/yaml.v3"
)

var bannerLines = []string{
	"-------------------------------",
	"░█▀▀▄░█▀▀▀░█▀▀░█▀▀▄░▀█▀░█▀█░█▀█",
	"▒█▄▄█░█░▀▄░█▀▀░█░▒█░░█░░▒▄▀░▒▄▀",
	"▒█░▒█░▀▀▀▀░▀▀▀░▀░░▀░░▀░░█▄▄░█▄▄",
	"-------------------------------",
	"Created by Cubixle",
}

const (
	ansiReset   = "\033[0m"
	ansiBlue    = "\033[38;5;39m"
	ansiCyan    = "\033[38;5;44m"
	ansiGreen   = "\033[38;5;42m"
	ansiSeafoam = "\033[38;5;49m"
)

func main() {
	pullRequestMode := flag.Bool("pull-request-mode", false, "monitor pull/merge request comments and apply changes via configured coding agent")

	flag.Parse()

	printBanner()

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

func printBanner() {
	if !supportsANSIColor() {
		fmt.Println(strings.Join(bannerLines, "\n"))
		return
	}

	lineColors := []string{ansiBlue, ansiGreen, ansiSeafoam, ansiCyan, ansiBlue, ansiGreen}
	for i, line := range bannerLines {
		fmt.Printf("%s%s%s\n", lineColors[i], line, ansiReset)
	}
}

func supportsANSIColor() bool {
	stat, err := os.Stdout.Stat()
	if err != nil {
		return false
	}

	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return false
	}

	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}

	term := strings.TrimSpace(strings.ToLower(os.Getenv("TERM")))

	return term != "" && term != "dumb"
}
