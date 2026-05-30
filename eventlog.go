package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// EventRecord represents a recorded event saved to disk.
type EventRecord struct {
	Timestamp string            `json:"timestamp"`
	Type      string            `json:"type"`
	Summary   string            `json:"summary,omitempty"`
	Payload   interface{}       `json:"payload,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type eventIndexEntry struct {
	File      string            `json:"file"`
	Timestamp string            `json:"timestamp"`
	Type      string            `json:"type"`
	Summary   string            `json:"summary,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

var (
	eventLogCh   chan EventRecord
	eventLogOnce sync.Once
)

func startEventLogger() {
	eventLogOnce.Do(func() {
		eventLogCh = make(chan EventRecord, 2048)
		SafeGo(func() {
			for rec := range eventLogCh {
				_ = writeEventRecord(rec)
			}
		})
	})
}

// LogEvent queues an event for asynchronous, durable write to disk.
func LogEvent(typ string, payload interface{}, summary string, metadata map[string]string) {
	startEventLogger()
	rec := EventRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Type:      typ,
		Summary:   summary,
		Payload:   payload,
		Metadata:  metadata,
	}
	select {
	case eventLogCh <- rec:
	default:
		// best-effort: if channel is full, we drop the log instead of spawning
		// infinite goroutines which causes OOM.
	}
}

func eventsBaseDir() string {
	cfgDir, _ := os.UserConfigDir()
	return filepath.Join(cfgDir, "roll-origins", "events")
}

func writeEventRecord(rec EventRecord) error {
	base := eventsBaseDir()
	dateDir := time.Now().UTC().Format("2006-01-02")
	dir := filepath.Join(base, dateDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	fname := fmt.Sprintf("%s__%s__%d.json", time.Now().UTC().Format("20060102T150405.000000Z"), rec.Type, rand.Intn(1000000))
	path := filepath.Join(dir, fname)
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		return err
	}

	// update per-day index (prepend newest)
	idxPath := filepath.Join(dir, "index.json")
	var idx []eventIndexEntry
	if data, err := os.ReadFile(idxPath); err == nil {
		_ = json.Unmarshal(data, &idx)
	}
	idx = append([]eventIndexEntry{{
		File:      filepath.Base(path),
		Timestamp: rec.Timestamp,
		Type:      rec.Type,
		Summary:   rec.Summary,
		Metadata:  rec.Metadata,
	}}, idx...)

	// Cap index to last 1000 events to prevent performance degradation and OOM
	// when reading/writing/marshalling huge index files.
	if len(idx) > 1000 {
		idx = idx[:1000]
	}

	if j, err := json.MarshalIndent(idx, "", "  "); err == nil {
		_ = os.WriteFile(idxPath, j, 0600)
	}
	return nil
}

// ListEventDatesJSON returns the available event date directories as JSON array.
func (a *App) ListEventDatesJSON() string {
	base := eventsBaseDir()
	entries, err := os.ReadDir(base)
	if err != nil {
		return "[]"
	}
	dates := []string{}
	for _, e := range entries {
		if e.IsDir() {
			dates = append(dates, e.Name())
		}
	}
	sort.Strings(dates)
	b, _ := json.Marshal(dates)
	return string(b)
}

// ListEventsForDateJSON returns the per-day index.json or builds one on the fly.
func (a *App) ListEventsForDateJSON(date string) string {
	dir := filepath.Join(eventsBaseDir(), date)
	idxPath := filepath.Join(dir, "index.json")
	if b, err := os.ReadFile(idxPath); err == nil {
		return string(b)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return "[]"
	}
	var idx []eventIndexEntry
	for _, f := range files {
		if f.IsDir() || f.Name() == "index.json" {
			continue
		}
		path := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec EventRecord
		if json.Unmarshal(data, &rec) == nil {
			idx = append(idx, eventIndexEntry{
				File:      f.Name(),
				Timestamp: rec.Timestamp,
				Type:      rec.Type,
				Summary:   rec.Summary,
				Metadata:  rec.Metadata,
			})
		}
	}
	b, _ := json.Marshal(idx)
	return string(b)
}

// ReadEventFileJSON returns the raw JSON content of a saved event file.
func (a *App) ReadEventFileJSON(date string, filename string) string {
	path := filepath.Join(eventsBaseDir(), date, filename)
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// ExportEventsForDate compiles all events for the given date into a
// human-readable text file and opens it in the platform default text
// editor (Notepad on Windows). It returns the path to the exported file
// or an empty string on failure.
func (a *App) ExportEventsForDate(date string) string {
	if strings.TrimSpace(date) == "" {
		return ""
	}
	dir := filepath.Join(eventsBaseDir(), date)
	files, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var events []EventRecord
	for _, f := range files {
		if f.IsDir() || f.Name() == "index.json" {
			continue
		}
		path := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec EventRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		events = append(events, rec)
	}

	if len(events) == 0 {
		return ""
	}

	sort.Slice(events, func(i, j int) bool { return events[i].Timestamp < events[j].Timestamp })

	var buf bytes.Buffer
	for _, rec := range events {
		buf.WriteString("============================================================\n")
		buf.WriteString(fmt.Sprintf("%s  |  %s\n", rec.Timestamp, rec.Type))
		if rec.Summary != "" {
			buf.WriteString(fmt.Sprintf("Summary: %s\n", rec.Summary))
		}
		if len(rec.Metadata) > 0 {
			buf.WriteString("Metadata:\n")
			for k, v := range rec.Metadata {
				buf.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
			}
		}
		if rec.Payload != nil {
			buf.WriteString("Payload:\n")
			if p, err := json.MarshalIndent(rec.Payload, "", "  "); err == nil {
				buf.Write(p)
				buf.WriteString("\n")
			} else {
				// Fallback: raw marshal of entire record
				if rb, err2 := json.MarshalIndent(rec, "", "  "); err2 == nil {
					buf.Write(rb)
					buf.WriteString("\n")
				}
			}
		}
		buf.WriteString("\n")
	}

	// Write to a temp file
	tmp, err := os.CreateTemp("", fmt.Sprintf("gamba-events-%s-*.txt", date))
	if err != nil {
		return ""
	}
	_, _ = tmp.Write(buf.Bytes())
	_ = tmp.Sync()
	_ = tmp.Close()

	// Open in platform editor (Notepad on Windows)
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("notepad.exe", tmp.Name()).Start()
	case "darwin":
		_ = exec.Command("open", tmp.Name()).Start()
	default:
		_ = exec.Command("xdg-open", tmp.Name()).Start()
	}

	return tmp.Name()
}
