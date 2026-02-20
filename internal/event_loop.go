// Package internal contains shared polling event loop helpers.
package internal

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type pollingEventLoopConfig struct {
	Name        string
	BaseWait    time.Duration
	MaxIdleWait time.Duration
	MaxTries    int
}

func runPollingEventLoop(config pollingEventLoopConfig, tick func(context.Context) (bool, error)) error {
	idleBackoffAttempt := 0
	tries := 0

	for {
		if config.MaxTries > 0 && tries >= config.MaxTries {
			slog.Info("Reached max loop tries, exiting", "loop", config.Name, "max_tries", config.MaxTries)
			return nil
		}

		tries++

		hadWork, err := tick(context.Background())
		if err != nil {
			return fmt.Errorf("run %s loop tick: %w", config.Name, err)
		}

		if hadWork {
			idleBackoffAttempt = 0
			continue
		}

		waitDuration := backoffDuration(config.BaseWait, idleBackoffAttempt, config.MaxIdleWait)
		slog.Debug("No work found, sleeping before next poll", "loop", config.Name, "sleep", waitDuration)
		outputWaitingForWork(waitDuration)

		idleBackoffAttempt++
	}
}

func backoffDuration(base time.Duration, attempt int, maxWait time.Duration) time.Duration {
	if base <= 0 {
		base = 30 * time.Second
	}

	if maxWait <= 0 {
		maxWait = 10 * time.Minute
	}

	if attempt < 0 {
		attempt = 0
	}

	if attempt > 8 {
		attempt = 8
	}

	backoff := base
	for range attempt {
		if backoff >= maxWait {
			return maxWait
		}

		if backoff > maxWait/2 {
			return maxWait
		}

		backoff *= 2
	}

	if backoff > maxWait {
		return maxWait
	}

	return backoff
}
