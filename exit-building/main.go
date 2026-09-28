package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"database/sql"
	_ "modernc.org/sqlite"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
)

//go:embed all:frontend/dist
var assets embed.FS

var DB_URL = "./database.sqlite"

func readLocalDBURL() string {
	if envURL := strings.TrimSpace(os.Getenv("ROLL_ORIGINS_DB_URL")); envURL != "" {
		return strings.TrimPrefix(envURL, "file:")
	}
	starts := []string{}
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	seen := make(map[string]struct{})
	for _, start := range starts {
		dir, err := filepath.Abs(start)
		if err != nil {
			continue
		}
		for depth := 0; depth < 10; depth++ {
			if _, ok := seen[dir]; !ok {
				seen[dir] = struct{}{}
				configPath := filepath.Join(dir, "db.local.json")
				if data, err := os.ReadFile(configPath); err == nil {
					var cfg struct{ DatabaseURL string `json:"databaseUrl"` }
					if json.Unmarshal(data, &cfg) == nil && strings.TrimSpace(cfg.DatabaseURL) != "" {
						dbPath := strings.TrimPrefix(strings.TrimSpace(cfg.DatabaseURL), "file:")
						if !filepath.IsAbs(dbPath) {
							dbPath = filepath.Join(dir, dbPath)
						}
						return dbPath
					}
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	exeDir := "."
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	return filepath.Join(exeDir, "database.sqlite")
}

type App struct {
	ctx     context.Context
	ext     *g.Ext
	mu      sync.Mutex
	logs    []string
	running bool

	// Database pool
	dbPool *sql.DB

	// Scheduling state
	scheduleMu      sync.Mutex
	scheduleEnabled bool
	targetTime      time.Time
}

func NewApp() *App {
	return &App{
		logs: []string{"Exit-Building initialized..."},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Initialize Database Pool
	dbCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dbPath := readLocalDBURL()
	pool, err := sql.Open("sqlite", dbPath)
	if err != nil {
		a.AddLog(fmt.Sprintf("CRITICAL ERROR: Failed to connect to DB: %v", err))
	} else if pingErr := pool.PingContext(dbCtx); pingErr != nil {
		a.AddLog(fmt.Sprintf("CRITICAL ERROR: DB ping failed: %v", pingErr))
	} else {
		a.dbPool = pool
		a.AddLog("Connected to local SQLite database successfully.")
	}

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Exit-Building",
		Description: "Automatic room exit with safety checks",
		Version:     "1.0.0",
		Author:      "Gemini CLI",
	})

	// Register headers
	a.ext.Headers().Add("QUIT", g.Header{Dir: g.Out, Value: 53})

	a.ext.Activated(func() {
		a.ShowWindow()
		clientType := "Unknown"
		if a.ext != nil {
			clientType = fmt.Sprintf("%v", a.ext.Client())
		}
		a.AddLog(fmt.Sprintf("Extension activated! Connected to: %s", clientType))
	})

	// Start background schedule monitor
	go a.monitorSchedule()

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
}

func (a *App) monitorSchedule() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		a.scheduleMu.Lock()
		if !a.scheduleEnabled {
			a.scheduleMu.Unlock()
			continue
		}

		if time.Now().After(a.targetTime) {
			a.scheduleEnabled = false
			a.AddLog(fmt.Sprintf(">>> SCHEDULE TRIGGERED at %s! <<<", time.Now().Format("15:04:05")))
			a.scheduleMu.Unlock()

			// Trigger exit sequence
			go a.ExecuteExit()
		} else {
			a.scheduleMu.Unlock()
		}
	}
}

func (a *App) SetSchedule(hours int, minutes int, enabled bool) {
	a.scheduleMu.Lock()
	defer a.scheduleMu.Unlock()

	if !enabled {
		a.scheduleEnabled = false
		a.AddLog("Schedule Disabled.")
		return
	}

	offset := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute
	if offset <= 0 {
		a.AddLog("ERROR: Schedule time must be greater than 0.")
		return
	}

	a.targetTime = time.Now().Add(offset)
	a.scheduleEnabled = true
	a.AddLog(fmt.Sprintf("Schedule Enabled! Bot will trigger in %dh %dm (at %s)", hours, minutes, a.targetTime.Format("15:04:05")))
}

type ScheduleStatus struct {
	TargetUnix int64 `json:"targetUnix"`
	Enabled    bool  `json:"enabled"`
}

func (a *App) GetScheduleStatus() ScheduleStatus {
	a.scheduleMu.Lock()
	defer a.scheduleMu.Unlock()

	unix := int64(0)
	if !a.targetTime.IsZero() {
		unix = a.targetTime.Unix()
	}

	return ScheduleStatus{
		TargetUnix: unix,
		Enabled:    a.scheduleEnabled,
	}
}

func (a *App) checkActiveGames() (bool, error) {
	if a.dbPool == nil {
		return false, fmt.Errorf("database not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var count int
	err := a.dbPool.QueryRowContext(ctx, "SELECT COUNT(*) FROM banker_trades WHERE status != 'completed'").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *App) ExecuteExit() {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		a.AddLog("Exit sequence already in progress...")
		return
	}
	a.running = true
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.running = false
			a.mu.Unlock()
		}()

		a.AddLog(">>> Starting safety checks for room exit...")

		for {
			active, err := a.checkActiveGames()
			if err != nil {
				a.AddLog(fmt.Sprintf("DB ERROR: %v. Waiting 30s to retry...", err))
				time.Sleep(30 * time.Second)
				continue
			}

			if !active {
				a.AddLog("No active games found. Safe to exit.")
				break
			}

			a.AddLog("[GUARD] Active games detected in database. Waiting 30s for completion...")
			time.Sleep(30 * time.Second)
		}

		// Send QUIT packet
		a.AddLog("Sending QUIT packet to server...")
		if a.ext != nil {
			// Header 53 (QUIT), Data: @u (0x40 0x75)
			a.ext.Send(g.Out.Id("QUIT"), []byte{0x40, 0x75})
			a.AddLog(">>> EXIT COMPLETED! Returning to Main Menu. <<<")
		} else {
			a.AddLog("ERROR: Extension not initialized")
		}
	}()
}

func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
	}
}

func (a *App) AddLog(msg string) {
	fullMsg := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	log.Println(fullMsg)
	a.mu.Lock()
	a.logs = append(a.logs, fullMsg)
	if len(a.logs) > 50 {
		a.logs = a.logs[len(a.logs)-50:]
	}
	logsCopy := make([]string, len(a.logs))
	copy(logsCopy, a.logs)
	a.mu.Unlock()
	if a.ctx != nil {
		go runtime.EventsEmit(a.ctx, "logsUpdate", logsCopy)
	}
}

func (a *App) GetLogs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.logs
}

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:  "Exit-Building",
		Width:  400,
		Height: 500,
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
