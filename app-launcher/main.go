package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// 笏笏 Build target definitions 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

type buildTarget struct {
	ID         string
	Name       string
	SrcDir     string   // relative to workspace root ("." = root itself)
	BuildType  string   // "wails" | "go"
	CleanPaths []string // relative to workspace root, deleted before each build
	OutExe     string   // relative to workspace root, expected output path
}

func knownBuildTargets() []buildTarget {
	return []buildTarget{
		{
			ID:        "roll-origins",
			Name:      "roll-origins",
			SrcDir:    ".",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("build", "bin", "roll-origins.exe"),
				// also wipe old name if it lingers
				filepath.Join("build", "bin", "Gamba-Suite.exe"),
			},
			OutExe: filepath.Join("build", "bin", "roll-origins.exe"),
		},
		{
			ID:        "wave-timer",
			Name:      "Wave Timer",
			SrcDir:    "wave-timer-app",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("wave-timer-app", "build", "bin", "wave-timer-app.exe"),
				filepath.Join("wave-timer-app", "wave-timer-app.exe"),
				filepath.Join("wave-timer-app", "wave-timer.exe"),
			},
			OutExe: filepath.Join("wave-timer-app", "build", "bin", "wave-timer-app.exe"),
		},
	}
}

// 笏笏 App types 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

type LaunchAppItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Running bool   `json:"running"`
}

type GEarthStatus struct {
	Exists  bool `json:"exists"`
	Running bool `json:"running"`
}

type App struct {
	ctx       context.Context
	mu        sync.Mutex
	apps      []LaunchAppItem
	processes map[string]*os.Process
	building  bool
}

func NewApp() *App {
	return &App{processes: map[string]*os.Process{}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.RefreshApps()
}

// 笏笏 Workspace root detection 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

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
		// workspace root has both app.go and main.go side by side
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
	words := strings.Fields(base)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// 笏笏 App discovery 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

func (a *App) discoverApps() []LaunchAppItem {
	root := a.resolveWorkspaceRoot()
	targets := knownBuildTargets()

	items := make([]LaunchAppItem, 0)
	seenPath := map[string]struct{}{}

	for _, t := range targets {
		candidates := []string{}
		if t.OutExe != "" {
			candidates = append(candidates, filepath.Join(root, t.OutExe))
		}
		for _, cp := range t.CleanPaths {
			candidates = append(candidates, filepath.Join(root, cp))
		}
		foundPath := ""
		for _, c := range candidates {
			if fileExists(c) {
				foundPath = c
				break
			}
		}
		item := LaunchAppItem{ID: t.ID, Name: t.Name, Path: foundPath, Exists: foundPath != ""}
		if foundPath != "" {
			seenPath[strings.ToLower(foundPath)] = struct{}{}
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Exists != items[j].Exists {
			return items[i].Exists
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	a.mu.Lock()
	for i := range items {
		if a.hasRunningInstanceLocked(items[i].ID) {
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

func (a *App) gEarthExePath() string {
	root := a.resolveWorkspaceRoot()
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	candidates := []string{
		filepath.Join(root, "G-Earth.windows-x64", "G-Earth.exe"),
		filepath.Join(root, "app-launcher", "G-Earth.windows-x64", "G-Earth.exe"),
		filepath.Join(exeDir, "G-Earth.windows-x64", "G-Earth.exe"),
	}

	for _, c := range candidates {
		if fileExists(c) {
			return c
		}
	}

	// Default expected location when it is not present yet.
	return candidates[0]
}

func (a *App) GetGEarthStatus() GEarthStatus {
	exePath := a.gEarthExePath()
	return GEarthStatus{Exists: fileExists(exePath), Running: false}
}

// 笏笏 Launch 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

func buildInstanceKey(appID, port string) string {
	if strings.TrimSpace(port) == "" {
		return appID + "|default"
	}
	return appID + "|" + strings.TrimSpace(port)
}

func (a *App) hasRunningInstanceLocked(appID string) bool {
	prefix := appID + "|"
	for key, proc := range a.processes {
		if proc != nil && strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func normalizePort(raw string) (string, error) {
	port := strings.TrimSpace(raw)
	if port == "" {
		return "", nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("port must be a number between 1 and 65535")
	}
	return strconv.Itoa(n), nil
}

func (a *App) LaunchApp(appID string, port string) string {
	item, err := a.findAppByID(strings.TrimSpace(appID))
	if err != nil {
		return err.Error()
	}
	if !item.Exists || strings.TrimSpace(item.Path) == "" {
		return "executable not found 窶・build it first"
	}

	normalizedPort, err := normalizePort(port)
	if err != nil {
		return err.Error()
	}

	args := []string{}
	if normalizedPort != "" {
		args = []string{"-p", normalizedPort}
	}

	instanceKey := buildInstanceKey(item.ID, normalizedPort)

	a.mu.Lock()
	if p, ok := a.processes[instanceKey]; ok && p != nil {
		a.mu.Unlock()
		if normalizedPort == "" {
			return "default instance is already running"
		}
		return "that port is already running for this app"
	}
	a.mu.Unlock()

	cmd := exec.Command(item.Path, args...)
	cmd.Dir = filepath.Dir(item.Path)
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("failed to launch: %v", err)
	}

	a.mu.Lock()
	a.processes[instanceKey] = cmd.Process
	a.mu.Unlock()

	go func(key string, c *exec.Cmd) {
		_ = c.Wait()
		a.mu.Lock()
		delete(a.processes, key)
		a.mu.Unlock()
	}(instanceKey, cmd)

	a.RefreshApps()
	return "ok"
}

func (a *App) LaunchGEarth() string {
	exePath := a.gEarthExePath()
	if !fileExists(exePath) {
		return "G-Earth.exe not found at G-Earth.windows-x64/G-Earth.exe"
	}

	if runtime.GOOS == "windows" {
		exeEscaped := strings.ReplaceAll(exePath, "'", "''")
		dirEscaped := strings.ReplaceAll(filepath.Dir(exePath), "'", "''")
		psCmd := fmt.Sprintf("Start-Process -FilePath '%s' -WorkingDirectory '%s' -Verb RunAs", exeEscaped, dirEscaped)
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
		if err := cmd.Run(); err != nil {
			return fmt.Sprintf("failed to launch G-Earth as admin: %v", err)
		}
		return "ok"
	}

	cmd := exec.Command(exePath)
	cmd.Dir = filepath.Dir(exePath)
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("failed to launch G-Earth: %v", err)
	}

	go func(c *exec.Cmd) {
		_ = c.Wait()
	}(cmd)

	return "ok"
}

// 笏笏 Build helpers 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

func (a *App) emitLog(line, kind string) {
	if a.ctx == nil {
		return
	}
	wailsrt.EventsEmit(a.ctx, "buildLog", map[string]string{
		"line": line,
		"kind": kind,
	})
}

// findTool looks up a CLI tool in PATH then common Windows install locations.
func findTool(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	if runtime.GOOS == "windows" {
		extras := []string{
			filepath.Join(os.Getenv("GOPATH"), "bin", name+".exe"),
			filepath.Join(os.Getenv("USERPROFILE"), "go", "bin", name+".exe"),
		}
		for _, e := range extras {
			if fileExists(e) {
				return e
			}
		}
	}
	return name
}

func (a *App) runCmd(dir string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		kind := "info"
		low := strings.ToLower(line)
		if strings.Contains(low, "error") || strings.Contains(low, "failed") || strings.Contains(low, "cannot") {
			kind = "error"
		}
		a.emitLog("    "+line, kind)
	}
	return err
}

func (a *App) buildOne(t buildTarget, root string) error {
	// Delete old exes before building
	for _, rel := range t.CleanPaths {
		full := filepath.Join(root, rel)
		if fileExists(full) {
			a.emitLog(fmt.Sprintf("  rm  %s", rel), "info")
			if err := os.Remove(full); err != nil {
				a.emitLog(fmt.Sprintf("  warn: could not remove %s: %v", rel, err), "error")
			}
		}
	}

	srcDir := filepath.Join(root, t.SrcDir)
	switch t.BuildType {
	case "wails":
		exe := findTool("wails")
		a.emitLog(fmt.Sprintf("  wails build  [dir: %s]", t.SrcDir), "info")
		return a.runCmd(srcDir, exe, "build")
	case "go":
		exe := findTool("go")
		a.emitLog(fmt.Sprintf("  go build .   [dir: %s]", t.SrcDir), "info")
		return a.runCmd(srcDir, exe, "build", ".")
	default:
		return fmt.Errorf("unknown build type %q", t.BuildType)
	}
}

// BuildAll removes old exes and rebuilds every known app.
// Progress is streamed as "buildLog" events; returns "building" immediately.
func (a *App) BuildAll() string {
	a.mu.Lock()
	if a.building {
		a.mu.Unlock()
		return "build already in progress"
	}
	a.building = true
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.building = false
			a.mu.Unlock()
			a.RefreshApps()
			a.emitLog("笊絶武笊絶武 All builds complete 笊絶武笊絶武", "success")
		}()

		root := a.resolveWorkspaceRoot()
		targets := knownBuildTargets()

		a.emitLog(fmt.Sprintf("Root: %s", root), "info")
		a.emitLog(fmt.Sprintf("Building %d app(s)窶ｦ", len(targets)), "info")

		for _, t := range targets {
			a.emitLog(fmt.Sprintf("笆ｶ [%s]", t.Name), "info")
			start := time.Now()
			if err := a.buildOne(t, root); err != nil {
				a.emitLog(fmt.Sprintf("笨・[%s] FAILED: %v", t.Name, err), "error")
			} else {
				a.emitLog(fmt.Sprintf("笨・[%s] done (%.1fs)", t.Name, time.Since(start).Seconds()), "success")
			}
		}
	}()

	return "building"
}

// BuildSingle removes old exe and rebuilds one app by ID.
func (a *App) BuildSingle(appID string) string {
	root := a.resolveWorkspaceRoot()
	targets := knownBuildTargets()

	var found *buildTarget
	for i := range targets {
		if targets[i].ID == appID {
			found = &targets[i]
			break
		}
	}
	if found == nil {
		return fmt.Sprintf("no build target for %q", appID)
	}

	a.mu.Lock()
	if a.building {
		a.mu.Unlock()
		return "build already in progress"
	}
	a.building = true
	a.mu.Unlock()

	go func(t buildTarget) {
		defer func() {
			a.mu.Lock()
			a.building = false
			a.mu.Unlock()
			a.RefreshApps()
			a.emitLog("笊絶武笊絶武 Build complete 笊絶武笊絶武", "success")
		}()

		a.emitLog(fmt.Sprintf("笆ｶ [%s]", t.Name), "info")
		start := time.Now()
		if err := a.buildOne(t, root); err != nil {
			a.emitLog(fmt.Sprintf("笨・[%s] FAILED: %v", t.Name, err), "error")
		} else {
			a.emitLog(fmt.Sprintf("笨・[%s] done (%.1fs)", t.Name, time.Since(start).Seconds()), "success")
		}
	}(*found)

	return "building"
}

// GetBuildTargets returns known build target IDs/names for the UI.
func (a *App) GetBuildTargets() []map[string]string {
	targets := knownBuildTargets()
	out := make([]map[string]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, map[string]string{"id": t.ID, "name": t.Name})
	}
	return out
}

// IsBuilding reports whether a build is currently running.
func (a *App) IsBuilding() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.building
}

// 笏笏 Main 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:             "App Launcher",
		Width:             680,
		Height:            600,
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
