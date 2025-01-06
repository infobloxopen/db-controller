package pgbouncer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

var logger logr.Logger

func init() {
	cfg := zap.NewProductionConfig()
	// Disable stack traces in this package
	cfg.EncoderConfig.StacktraceKey = ""

	zapLog, _ := cfg.Build()
	logger = zapr.NewLogger(zapLog)
}

func SetLogger(l logr.Logger) {
	logger = l
}

// Reload tells pgbouncer to reload its configuration
func Reload(ctx context.Context, scriptPath string) error {
	logger.Info("Reloading PG Bouncer config", "scriptPath", scriptPath)
	err := run(ctx, scriptPath, logger)
	return err
}

// Start executes a script to initialize pgbouncer
func Start(ctx context.Context, scriptPath string) error {
	logger.Info("Starting PG Bouncer", "scriptPath", scriptPath)
	err := run(ctx, scriptPath, logger)
	return err
}

func run(ctx context.Context, scriptPath string, logger logr.Logger) error {
	cmd := exec.CommandContext(ctx, scriptPath)

	// Create pipes for stdout and stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error creating stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("error creating stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting command: %w", err)
	}

	// Function to stream and log output
	streamAndLog := func(pipe io.ReadCloser, logFunc func(string, ...interface{})) {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			logFunc(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			logger.Error(err, "Error reading output")
		}
	}

	// Stream stdout and stderr concurrently
	go streamAndLog(stdoutPipe, func(msg string, args ...interface{}) {
		logger.Info(msg, args...)
	})
	go streamAndLog(stderrPipe, func(msg string, args ...interface{}) {
		logger.Error(nil, msg, args...)
	})

	// Wait for the command to finish
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("error waiting for command: %w", err)
	}

	return nil
}
