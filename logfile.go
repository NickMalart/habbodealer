package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

// InitLogging creates a timestamped log file under ./logs and ensures
// both the standard library logger and logrus write to it. It also
// captures stdout/stderr so fmt.Print* calls are persisted as well.
func InitLogging() {
	logsDir := "logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logs directory %s: %v\n", logsDir, err)
		return
	}

	fname := time.Now().Format("2006-01-02_15-04-05") + ".log"
	fpath := filepath.Join(logsDir, fname)
	file, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open log file %s: %v\n", fpath, err)
		return
	}

	// Keep a reference to the original stdout so we can still write to console.
	origStdout := os.Stdout

	// Multi-writer: console + file
	mw := io.MultiWriter(origStdout, file)

	// Standard library logger
	log.SetOutput(mw)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// logrus
	logrus.SetOutput(mw)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05.000",
	})

	// Redirect fmt/printf writes by replacing os.Stdout/os.Stderr with a pipe
	// and copying the pipe reader to the multi-writer (origStdout + file).
	r, w, err := os.Pipe()
	if err != nil {
		log.Printf("Failed to create stdout pipe: %v", err)
		return
	}

	os.Stdout = w
	os.Stderr = w

	go func() {
		_, _ = io.Copy(mw, r)
	}()

	log.Printf("Logging initialized; writing to %s", fpath)
}
