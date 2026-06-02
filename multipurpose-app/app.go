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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	"xabbo.b7c.io/goearth/shockwave/out"
)

const dbURL = "postgresql://neondb_owner:npg_S9jFTYzdQx3l@ep-aged-king-a77p1t8b-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"

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

	db *pgxpool.Pool

	autoGrantRights   bool
	lastGrantedRights map[string]time.Time

	logs   []string
	logsMu sync.Mutex
}

// NewApp creates a new App application struct
func NewApp(ext *g.Ext) *App {
	a := &App{
		ext:               ext,
		gearthStatus:      "disconnected",
		roomUsers:         make(map[int]RoomUser),
		lastGrantedRights: make(map[string]time.Time),
		logs:              make([]string, 0),
	}
	a.initParser()
	return a
}

// AddLog adds a log message and emits it to the frontend
func (a *App) AddLog(msg string) {
	fullMsg := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	a.logsMu.Lock()
	a.logs = append(a.logs, fullMsg)
	if len(a.logs) > 100 {
		a.logs = a.logs[len(a.logs)-100:]
	}
	a.logsMu.Unlock()

	log.Println(msg)
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "new_log", fullMsg)
	}
}

// GetLogs returns the stored logs
func (a *App) GetLogs() []string {
	a.logsMu.Lock()
	defer a.logsMu.Unlock()
	return a.logs
}

// ToggleAutoGrantRights enables or disables automatic rights assignment
func (a *App) ToggleAutoGrantRights(enabled bool) {
	a.autoGrantRights = enabled
	a.AddLog(fmt.Sprintf("Auto-grant toggled: %t", enabled))

	if enabled {
		// Immediately check current room users
		go a.checkAndGrantRightsToCurrentUsers()
	}

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "auto_grant_rights_updated", enabled)
	}
}

func (a *App) checkAndGrantRightsToCurrentUsers() {
	a.AddLog("Running immediate auto-grant check...")
	authorizedUsers, err := a.GetRoomRights()
	if err != nil {
		a.AddLog(fmt.Sprintf("ERROR: Failed to fetch authorized users from DB: %v", err))
		return
	}

	a.AddLog(fmt.Sprintf("Found %d authorized users in database.", len(authorizedUsers)))
	if len(authorizedUsers) == 0 {
		return
	}

	a.roomUsersMu.RLock()
	users := make([]RoomUser, 0, len(a.roomUsers))
	for _, u := range a.roomUsers {
		users = append(users, u)
	}
	a.roomUsersMu.RUnlock()

	a.AddLog(fmt.Sprintf("Checking %d users currently in room...", len(users)))
	for _, u := range users {
		isAuthorized := false
		for _, auth := range authorizedUsers {
			if strings.EqualFold(auth, u.Name) {
				isAuthorized = true
				break
			}
		}

		if isAuthorized {
			lastGrant, seen := a.lastGrantedRights[u.Name]
			if !seen || time.Since(lastGrant) > 2*time.Minute {
				a.AddLog(fmt.Sprintf("ACTION: Granting rights to %s (ChatID: %d)", u.Name, u.ChatID))

				// Packet format: "A" + ChatID (as raw byte) + Username
				payload := []byte("A")
				payload = append(payload, byte(u.ChatID))
				payload = append(payload, []byte(u.Name)...)

				a.ext.Send(g.Out.Id("ASSIGNRIGHTS"), payload)
				a.lastGrantedRights[u.Name] = time.Now()
			} else {
				a.AddLog(fmt.Sprintf("SKIP: %s already granted recently (%.1fs ago)", u.Name, time.Since(lastGrant).Seconds()))
			}
		}
	}
}

// GetAutoGrantRights returns whether auto-grant is enabled
func (a *App) GetAutoGrantRights() bool {
	return a.autoGrantRights
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
	go a.initDatabase()

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

func (a *App) initDatabase() {
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		a.AddLog(fmt.Sprintf("ERROR: Failed to parse database URL: %v", err))
		return
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		a.AddLog(fmt.Sprintf("ERROR: Failed to connect to database: %v", err))
		return
	}

	a.db = pool

	// Create table if not exists
	_, err = a.db.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS room_rights (
			id SERIAL PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			added_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		a.AddLog(fmt.Sprintf("ERROR: Database table initialization failed: %v", err))
	} else {
		a.AddLog("Database initialized and room_rights table verified.")
	}
}

// AddRoomRight adds a user to the room rights list in the database
func (a *App) AddRoomRight(username string) error {
	if a.db == nil {
		return fmt.Errorf("database not connected")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	a.AddLog(fmt.Sprintf("DB: Adding right for %s", username))
	_, err := a.db.Exec(context.Background(),
		"INSERT INTO room_rights (username) VALUES ($1) ON CONFLICT (username) DO NOTHING",
		username)
	if err != nil {
		a.AddLog(fmt.Sprintf("ERROR: DB add failed: %v", err))
	}
	return err
}

// RemoveRoomRight removes a user from the room rights list in the database
func (a *App) RemoveRoomRight(username string) error {
	if a.db == nil {
		return fmt.Errorf("database not connected")
	}
	a.AddLog(fmt.Sprintf("DB: Removing right for %s", username))
	_, err := a.db.Exec(context.Background(), "DELETE FROM room_rights WHERE username = $1", username)
	if err != nil {
		a.AddLog(fmt.Sprintf("ERROR: DB remove failed: %v", err))
	}
	return err
}

// GetRoomRights returns the list of usernames with room rights from the database
func (a *App) GetRoomRights() ([]string, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := a.db.Query(context.Background(), "SELECT username FROM room_rights ORDER BY username ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, err
		}
		users = append(users, username)
	}
	return users, nil
}

func (a *App) setupExt() {
	a.ext.Headers().Add("ASSIGNRIGHTS", g.Header{Dir: g.Out, Value: 96})

	a.ext.Initialized(func(e g.InitArgs) {
		a.AddLog(fmt.Sprintf("G-Earth initialized (connected=%t)", e.Connected))
		a.gearthStatus = "initialized"
		if e.Connected {
			a.gearthStatus = "connected"
		}
		a.emitStatus()
	})

	a.ext.Connected(func(e g.ConnectArgs) {
		a.AddLog(fmt.Sprintf("G-Earth connected to %s:%d", e.Host, e.Port))
		a.gearthStatus = "connected"
		a.gearthHost = e.Host
		a.gearthPort = e.Port
		a.emitStatus()
		a.RequestRoomUsers()
	})

	a.ext.Disconnected(func() {
		a.AddLog("G-Earth disconnected")
		a.gearthStatus = "disconnected"
		a.emitStatus()
		a.roomUsersMu.Lock()
		a.roomUsers = make(map[int]RoomUser)
		a.roomUsersMu.Unlock()
		runtime.EventsEmit(a.ctx, "room_users_updated", []RoomUser{})
	})
}

func (a *App) handleRoomUsers(e *g.Intercept) {
	// a.AddLog(fmt.Sprintf("DEBUG: Intercepted packet header=%d len=%d", e.Packet.Header.Value, len(e.Packet.Data)))

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
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		a.AddLog(fmt.Sprintf("ERROR: Parser failed: %v", err))
		return
	}

	var users []ParsedUsers28User
	if err := json.Unmarshal(stdout.Bytes(), &users); err != nil {
		a.AddLog(fmt.Sprintf("ERROR: Failed to parse user data: %v", err))
		return
	}

	// Get authorized users from DB if auto-grant is enabled
	var authorizedUsers []string
	if a.autoGrantRights {
		var err error
		authorizedUsers, err = a.GetRoomRights()
		if err != nil {
			a.AddLog(fmt.Sprintf("ERROR: Auto-grant authorized list fetch failed: %v", err))
		}
	}

	a.roomUsersMu.Lock()
	for _, u := range users {
		a.roomUsers[u.ChatID] = RoomUser{
			Name:    u.Username,
			ChatID:  u.ChatID,
			TradeID: u.TradeID,
		}

		// Auto-grant rights if enabled and user is authorized
		if a.autoGrantRights && len(authorizedUsers) > 0 {
			isAuthorized := false
			for _, auth := range authorizedUsers {
				if strings.EqualFold(auth, u.Username) {
					isAuthorized = true
					break
				}
			}

			if isAuthorized {
				lastGrant, seen := a.lastGrantedRights[u.Username]
				// Only grant if never granted or granted more than 2 minutes ago
				if !seen || time.Since(lastGrant) > 2*time.Minute {
					a.AddLog(fmt.Sprintf("ACTION: Auto-granting rights to %s (ChatID: %d)", u.Username, u.ChatID))

					// Packet format: "A" + ChatID (as raw byte) + Username
					payload := []byte("A")
					payload = append(payload, byte(u.ChatID))
					payload = append(payload, []byte(u.Username)...)

					a.ext.Send(g.Out.Id("ASSIGNRIGHTS"), payload)
					a.lastGrantedRights[u.Username] = time.Now()
				}
			}
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
