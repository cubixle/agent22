// Package internal provides terminal output helpers for agent workflows.
package internal

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	ansiReset  = "\033[0m"
	ansiBlue   = "\033[34m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

var (
	colorSupportOnce sync.Once
	colorEnabled     bool
	ttySupportOnce   sync.Once
	stdoutIsTTY      bool
)

func outputInfof(format string, args ...any) {
	printLabeledf("INFO", ansiBlue, format, args...)
}

func outputStepf(format string, args ...any) {
	printLabeledf("STEP", ansiCyan, format, args...)
}

func outputSuccessf(format string, args ...any) {
	printLabeledf("OK", ansiGreen, format, args...)
}

func outputWarnf(format string, args ...any) {
	printLabeledf("WARN", ansiYellow, format, args...)
}

func outputWaitingForWork(duration time.Duration) {
	if duration <= 0 {
		return
	}

	if !isStdoutTTY() {
		outputInfof("Waiting for work (%s)", duration.Round(time.Second))
		time.Sleep(duration)

		return
	}

	spinner := []string{"|", "/", "-", "\\"}
	ticker := time.NewTicker(150 * time.Millisecond)

	defer ticker.Stop()

	timer := time.NewTimer(duration)
	defer timer.Stop()

	index := 0

	for {
		label := "WAIT"
		if supportsANSIColor() {
			label = ansiYellow + label + ansiReset
		}

		frame := spinnerFrame(spinner, index)

		fmt.Printf("\r[%s] Waiting for work %s", label, frame)

		select {
		case <-timer.C:
			fmt.Print("\n")
			return
		case <-ticker.C:
			index++
		}
	}
}

func printLabeledf(label, labelColor, format string, args ...any) {
	message := fmt.Sprintf(strings.TrimSpace(format), args...)

	prefix := label
	if supportsANSIColor() {
		prefix = labelColor + label + ansiReset
	}

	fmt.Printf("[%s] %s\n", prefix, message)
}

func supportsANSIColor() bool {
	colorSupportOnce.Do(func() {
		if !isStdoutTTY() {
			colorEnabled = false
			return
		}

		if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
			colorEnabled = false
			return
		}

		term := strings.TrimSpace(strings.ToLower(os.Getenv("TERM")))
		if term == "" || term == "dumb" {
			colorEnabled = false
			return
		}

		colorEnabled = true
	})

	return colorEnabled
}

func isStdoutTTY() bool {
	ttySupportOnce.Do(func() {
		stat, err := os.Stdout.Stat()
		if err != nil {
			stdoutIsTTY = false
			return
		}

		stdoutIsTTY = (stat.Mode() & os.ModeCharDevice) != 0
	})

	return stdoutIsTTY
}
