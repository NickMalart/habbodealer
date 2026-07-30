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
	Version:     "1.0.1",
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

type AppState struct {
	Connected bool     `json:"connected"`
	Users     []string `json:"users"`
}

type App struct {
	ctx context.Context
	mu  sync.Mutex

	connected bool
	users     []string
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
	ext.Activated(func() {
		if a.ctx != nil {
			runtime.WindowShow(a.ctx)
		}
	})

	ext.Connected(func(g.ConnectArgs) {
		a.mu.Lock()
		a.connected = true
		a.mu.Unlock()
		a.emitUpdate()
	})

	ext.Disconnected(func() {
		a.mu.Lock()
		a.connected = false
		a.mu.Unlock()
		a.emitUpdate()
	})

	// Use InterceptAll to catch header 28 manually since it's the most reliable for Origins room users
	ext.InterceptAll(func(e *g.Intercept) {
		if e.Packet.Header.Dir == g.In && e.Packet.Header.Value == 28 {
			a.handleUsersPacket(e)
		}
	})

	// Also standard USERS/SPACENODEUSERS
	ext.Intercept(in.USERS, in.SPACENODEUSERS).With(a.handleUsersPacket)

	ext.Run()
}

func (a *App) handleUsersPacket(e *g.Intercept) {
	if e == nil || e.Packet == nil || len(e.Packet.Data) == 0 {
		return
	}

	// COPY data to avoid race conditions with the packet thread and allow async processing
	data := make([]byte, len(e.Packet.Data))
	copy(data, e.Packet.Data)

	go func() {
		// We use the Python parser to handle the complex USERS28 format
		users, err := a.runUsers28PythonParser(data)
		if err != nil {
			log.Printf("Parser failed: %v", err)
			return
		}

		a.mu.Lock()
		userMap := make(map[string]bool)
		// Maintain existing if they are still there
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

		a.emitUpdate()
	}()
}

func (a *App) emitUpdate() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "state_updated", a.GetState())
}

func (a *App) GetState() AppState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AppState{
		Connected: a.connected,
		Users:     a.users,
	}
}

func (a *App) UpdateUsers() {
	ext.Send(out.G_USRS)
	ext.Send(out.GETSPACENODEUSERS)
	
	// Clear local list when requesting fresh to see who is actually there
	a.mu.Lock()
	a.users = []string{}
	a.mu.Unlock()
	a.emitUpdate()
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

func (a *App) runUsers28PythonParser(packetData []byte) ([]ParsedUsers28User, error) {
	// Try to find the script in common locations
	scriptPath := filepath.Join("..", "scripts", "parse_users28.py")
	if _, err := os.Stat(scriptPath); err != nil {
		scriptPath = filepath.Join("scripts", "parse_users28.py")
	}
	
	if _, err := os.Stat(scriptPath); err != nil {
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
	args := []string{}
	if p, err := exec.LookPath("py"); err == nil {
		py = p
		args = []string{"-3"}
	} else if _, err := exec.LookPath("python3"); err == nil {
		py = "python3"
	}

	cmdArgs := append(args, scriptPath, "--input", tmpPath, "--json")
	cmd := exec.Command(py, cmdArgs...)
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
		Width:  420,
		Height: 620,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
		BackgroundColour: &options.RGBA{R: 18, G: 22, B: 28, A: 1},
	})

	if err != nil {
		log.Fatal(err)
	}
}
