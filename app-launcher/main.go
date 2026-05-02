package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

type LaunchAppItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Running bool   `json:"running"`
}

type App struct {
	ctx context.Context
	mu  sync.Mutex

	apps      []LaunchAppItem
	processes map[string]*os.Process
}

func NewApp() *App {
	return &App{processes: map[string]*os.Process{}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.RefreshApps()
}

func (a *App) resolveWorkspaceRoot() string {
	cwd, _ := os.Getwd()
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	candidates := []string{cwd, exeDir, filepath.Dir(cwd), filepath.Dir(exeDir)}
	seen := map[string]struct{}{}

	for _, c := range candidates {
		if c == "" {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		if fileExists(filepath.Join(abs, "app.go")) && fileExists(filepath.Join(abs, "main.go")) {
			return abs
		}
	}

	if cwd != "" {
		return cwd
	}
	if exeDir != "" {
		return exeDir
	}
	return "."
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func canonicalNameFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.TrimSpace(base)
	if base == "" {
		return "Unknown App"
	}
	return strings.Title(base)
}

func (a *App) discoverApps() []LaunchAppItem {
	root := a.resolveWorkspaceRoot()

	// Preferred known tools first.
	known := []struct {
		ID         string
		Name       string
		Candidates []string
	}{
		{
			ID:   "roll-origins",
			Name: "roll-origins",
			Candidates: []string{
				filepath.Join(root, "build", "bin", "roll-origins.exe"),
				filepath.Join(root, "roll-origins.exe"),
				filepath.Join(root, "build", "bin", "Gamba-Suite.exe"),
			},
		},
		{
			ID:   "wave-timer",
			Name: "Wave Timer",
			Candidates: []string{
				filepath.Join(root, "wave-timer-app", "wave-timer-app.exe"),
				filepath.Join(root, "wave-timer-app", "wave-timer.exe"),
			},
		},
	}

	items := make([]LaunchAppItem, 0)
	seenPath := map[string]struct{}{}

	for _, k := range known {
		foundPath := ""
		for _, c := range k.Candidates {
			if fileExists(c) {
				foundPath = c
				break
			}
		}
		item := LaunchAppItem{ID: k.ID, Name: k.Name, Path: foundPath, Exists: foundPath != ""}
		if foundPath != "" {
			seenPath[strings.ToLower(foundPath)] = struct{}{}
		}
		items = append(items, item)
	}

	// Also auto-discover exes in workspace root, build/bin and sibling tool dirs.
	scanDirs := []string{
		root,
		filepath.Join(root, "build", "bin"),
		filepath.Join(root, "wave-timer-app"),
	}

	for _, dir := range scanDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".exe") {
				continue
			}
			full := filepath.Join(dir, name)
			if strings.EqualFold(name, "app-launcher.exe") {
				continue
			}
			if _, ok := seenPath[strings.ToLower(full)]; ok {
				continue
			}
			id := strings.TrimSuffix(strings.ToLower(name), ".exe")
			items = append(items, LaunchAppItem{
				ID:     id,
				Name:   canonicalNameFromPath(name),
				Path:   full,
				Exists: true,
			})
			seenPath[strings.ToLower(full)] = struct{}{}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Exists != items[j].Exists {
			return items[i].Exists
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	a.mu.Lock()
	for i := range items {
		if p, ok := a.processes[items[i].ID]; ok && p != nil {
			items[i].Running = true
		}
	}
	a.mu.Unlock()

	return items
}

func (a *App) RefreshApps() []LaunchAppItem {
	apps := a.discoverApps()
	a.mu.Lock()
	a.apps = apps
	a.mu.Unlock()
	return apps
}

func (a *App) GetApps() []LaunchAppItem {
	a.mu.Lock()
	if len(a.apps) > 0 {
		apps := make([]LaunchAppItem, len(a.apps))
		copy(apps, a.apps)
		a.mu.Unlock()
		return apps
	}
	a.mu.Unlock()
	return a.RefreshApps()
}

func (a *App) findAppByID(id string) (LaunchAppItem, error) {
	apps := a.GetApps()
	for _, it := range apps {
		if it.ID == id {
			return it, nil
		}
	}
	return LaunchAppItem{}, errors.New("app not found")
}

func (a *App) LaunchApp(appID string) string {
	item, err := a.findAppByID(strings.TrimSpace(appID))
	if err != nil {
		return err.Error()
	}
	if !item.Exists || strings.TrimSpace(item.Path) == "" {
		return "selected app executable not found"
	}

	a.mu.Lock()
	if p, ok := a.processes[item.ID]; ok && p != nil {
		a.mu.Unlock()
		return "app is already running"
	}
	a.mu.Unlock()

	cmd := exec.Command(item.Path)
	cmd.Dir = filepath.Dir(item.Path)
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("failed to launch: %v", err)
	}

	a.mu.Lock()
	a.processes[item.ID] = cmd.Process
	a.mu.Unlock()

	go func(id string, c *exec.Cmd) {
		_ = c.Wait()
		a.mu.Lock()
		delete(a.processes, id)
		a.mu.Unlock()
	}(item.ID, cmd)

	a.RefreshApps()
	return "ok"
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:             "App Launcher",
		Width:             560,
		Height:            460,
		MinWidth:          560,
		MinHeight:         460,
		DisableResize:     false,
		StartHidden:       false,
		HideWindowOnClose: false,
		OnStartup:         app.startup,
		Bind:              []interface{}{app},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 14, G: 18, B: 25, A: 1},
	})

	if err != nil {
		panic(err)
	}
}
