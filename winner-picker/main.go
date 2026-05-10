package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	in "xabbo.b7c.io/goearth/shockwave/in"
	out "xabbo.b7c.io/goearth/shockwave/out"
)

//go:embed all:frontend/dist
var assets embed.FS

var ext = g.NewExt(g.ExtInfo{
	Title:       "Winner Picker",
	Description: "Picks a random winner from users in the room",
	Version:     "1.0.0",
	Author:      "Dubbo",
})

type ParsedUsers28User struct {
	Username     string `json:"username"`
	TradeID      int    `json:"trade_id"`
	TradeIDRaw   string `json:"trade_id_raw"`
	ChatID       int    `json:"chat_id"`
	ChatIDRaw    string `json:"chat_id_raw"`
	EntityID     string `json:"entity_id,omitempty"`
	Figure       string `json:"figure,omitempty"`
	Sex          string `json:"sex,omitempty"`
	Motto        string `json:"motto,omitempty"`
	TokenHex     string `json:"token_hex,omitempty"`
	RawNameBlock string `json:"raw_name_block,omitempty"`
}

type App struct {
	ctx context.Context
	mu  sync.Mutex

	users []string
}

func NewApp() *App {
	return &App{
		users: []string{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.runExt()
}

func (a *App) runExt() {
	ext.Intercept(in.USERS, in.SPACENODEUSERS).With(a.handleUsersPacket)
	
	// Also intercept header 28 specifically as it's common in Origins
	ext.Intercept(g.In.Id("28")).With(a.handleUsersPacket)

	ext.Run()
}

func (a *App) handleUsersPacket(e *g.Intercept) {
	if e == nil || e.Packet == nil || len(e.Packet.Data) == 0 {
		return
	}

	// We use the Python parser to handle the complex USERS28 format
	users, err := a.runUsers28PythonParser(e.Packet.Data)
	if err != nil {
		log.Printf("Parser failed: %v", err)
		return
	}

	a.mu.Lock()
	userMap := make(map[string]bool)
	// Add existing users to map to avoid duplicates if multiple packets arrive
	for _, u := range a.users {
		userMap[u] = true
	}
	
	for _, u := range users {
		name := strings.TrimSpace(u.Username)
		if name != "" {
			userMap[name] = true
		}
	}
	
	finalUsers := []string{}
	for name := range userMap {
		finalUsers = append(finalUsers, name)
	}
	sort.Strings(finalUsers)
	a.users = finalUsers
	a.mu.Unlock()

	runtime.EventsEmit(a.ctx, "users_updated", a.users)
}

func (a *App) UpdateUsers() {
	ext.Send(out.G_USRS)
	ext.Send(out.GETSPACENODEUSERS)
	
	// Clear local list when requesting fresh
	a.mu.Lock()
	a.users = []string{}
	a.mu.Unlock()
	runtime.EventsEmit(a.ctx, "users_updated", a.users)
}

func (a *App) PickWinner(blockedNames []string) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.users) == 0 {
		return ""
	}

	blockedMap := make(map[string]bool)
	for _, name := range blockedNames {
		blockedMap[strings.ToLower(strings.TrimSpace(name))] = true
	}

	eligible := []string{}
	for _, name := range a.users {
		if !blockedMap[strings.ToLower(strings.TrimSpace(name))] {
			eligible = append(eligible, name)
		}
	}

	if len(eligible) == 0 {
		return ""
	}

	winner := eligible[rand.Intn(len(eligible))]
	return winner
}

func (a *App) ShoutWinner(name string) {
	msg := fmt.Sprintf("%s You have won a prize!", name)
	ext.Send(out.SHOUT, msg)
}

func (a *App) GetUsers() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.users
}

func (a *App) runUsers28PythonParser(packetData []byte) ([]ParsedUsers28User, error) {
	// Try to find the script in common locations
	scriptPath := filepath.Join("..", "scripts", "parse_users28.py")
	if _, err := os.Stat(scriptPath); err != nil {
		scriptPath = filepath.Join("scripts", "parse_users28.py")
	}
	
	if _, err := os.Stat(scriptPath); err != nil {
		// Try absolute path from executable if relative fails
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)
		scriptPath = filepath.Join(exeDir, "..", "..", "..", "scripts", "parse_users28.py")
	}

	tmpFile, err := os.CreateTemp("", "users28_*.bin")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(packetData); err != nil {
		tmpFile.Close()
		return nil, err
	}
	tmpFile.Close()

	py := "python"
	if _, err := exec.LookPath("python3"); err == nil {
		py = "python3"
	}

	cmd := exec.Command(py, scriptPath, "--input", tmpPath, "--json")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var users []ParsedUsers28User
	if err := json.Unmarshal(stdout.Bytes(), &users); err != nil {
		return nil, err
	}

	return users, nil
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Winner Picker",
		Width:  400,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
