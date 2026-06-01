package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	"xabbo.b7c.io/goearth/shockwave/out"
)

// ParsedUsers28User matches the JSON output of parse_users28.py
type ParsedUsers28User struct {
	Username string `json:"username"`
	TradeID  int    `json:"trade_id"`
	ChatID   int    `json:"chat_id"`
	EntityID string `json:"entity_id,omitempty"`
}

type RoomUser struct {
	Name    string `json:"name"`
	ChatID  int    `json:"chat_id"`
	TradeID int    `json:"trade_id"`
}

// App struct
type App struct {
	ctx          context.Context
	ext          *g.Ext
	gearthStatus string
	gearthHost   string
	gearthPort   int

	roomUsers   map[int]RoomUser // ChatID -> RoomUser
	roomUsersMu sync.RWMutex

	pythonExec   string
	parserScript string
}

// NewApp creates a new App application struct
func NewApp(ext *g.Ext) *App {
	a := &App{
		ext:          ext,
		gearthStatus: "disconnected",
		roomUsers:    make(map[int]RoomUser),
	}
	a.initParser()
	return a
}

func (a *App) initParser() {
	// Try to find python
	if p, err := exec.LookPath("python3"); err == nil {
		a.pythonExec = p
	} else if p, err := exec.LookPath("python"); err == nil {
		a.pythonExec = p
	}

	// Resolve parser script path relative to workspace root
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	candidates := []string{
		filepath.Join(exeDir, "..", "..", "..", "scripts", "parse_users28.py"),
		filepath.Join("..", "scripts", "parse_users28.py"),
		filepath.Join("scripts", "parse_users28.py"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			a.parserScript = c
			break
		}
	}
}

// startup is called when the app starts. The context is saved
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.setupExt()
	go a.runExt()

	// Periodically request room users if connected
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for {
			select {
			case <-ticker.C:
				if a.gearthStatus == "connected" {
					a.RequestRoomUsers()
				}
			case <-a.ctx.Done():
				return
			}
		}
	}()
}

func (a *App) setupExt() {
	a.ext.Initialized(func(e g.InitArgs) {
		log.Printf("G-Earth initialized (connected=%t)", e.Connected)
		a.gearthStatus = "initialized"
		if e.Connected {
			a.gearthStatus = "connected"
		}
		a.emitStatus()
	})

	a.ext.Connected(func(e g.ConnectArgs) {
		log.Printf("G-Earth connected (%s:%d)", e.Host, e.Port)
		a.gearthStatus = "connected"
		a.gearthHost = e.Host
		a.gearthPort = e.Port
		a.emitStatus()
		a.RequestRoomUsers()
	})

	a.ext.Disconnected(func() {
		log.Printf("G-Earth disconnected")
		a.gearthStatus = "disconnected"
		a.emitStatus()
		a.roomUsersMu.Lock()
		a.roomUsers = make(map[int]RoomUser)
		a.roomUsersMu.Unlock()
		runtime.EventsEmit(a.ctx, "room_users_updated", []RoomUser{})
	})
}

func (a *App) handleRoomUsers(e *g.Intercept) {
	if a.parserScript == "" || a.pythonExec == "" {
		return
	}

	tmpFile, err := os.CreateTemp("", "users28_*.bin")
	if err != nil {
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(e.Packet.Data); err != nil {
		tmpFile.Close()
		return
	}
	tmpFile.Close()

	cmd := exec.Command(a.pythonExec, a.parserScript, "--input", tmpPath, "--json")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout bytes.Buffer
	if err := cmd.Run(); err != nil {
		return
	}

	var users []ParsedUsers28User
	if err := json.Unmarshal(stdout.Bytes(), &users); err != nil {
		return
	}

	a.roomUsersMu.Lock()
	// If it's a full USERS packet (header 28), we might want to clear old ones, 
	// but USERS28 often comes in fragments or updates. 
	// For simplicity, we'll just update/add.
	for _, u := range users {
		a.roomUsers[u.ChatID] = RoomUser{
			Name:    u.Username,
			ChatID:  u.ChatID,
			TradeID: u.TradeID,
		}
	}
	a.roomUsersMu.Unlock()

	runtime.EventsEmit(a.ctx, "room_users_updated", a.GetRoomUsers())
}

// RequestRoomUsers sends packets to G-Earth to request the current room user list.
func (a *App) RequestRoomUsers() {
	if a.ext == nil {
		return
	}
	a.ext.Send(out.G_USRS)
	a.ext.Send(out.GETSPACENODEUSERS)
}

// GetRoomUsers returns a sorted list of users currently in the room.
func (a *App) GetRoomUsers() []RoomUser {
	a.roomUsersMu.RLock()
	defer a.roomUsersMu.RUnlock()

	users := make([]RoomUser, 0, len(a.roomUsers))
	for _, u := range a.roomUsers {
		users = append(users, u)
	}

	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i].Name) < strings.ToLower(users[j].Name)
	})

	return users
}

func (a *App) emitStatus() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "gearth_status", a.GetGEarthStatus())
}

// GetGEarthStatus returns the current G-Earth connection status
func (a *App) GetGEarthStatus() map[string]interface{} {
	return map[string]interface{}{
		"status": a.gearthStatus,
		"host":   a.gearthHost,
		"port":   a.gearthPort,
	}
}

func (a *App) runExt() {
	a.ext.Run()
}

// ShowWindow shows the application window
func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
	}
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

