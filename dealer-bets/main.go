package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	in "xabbo.b7c.io/goearth/shockwave/in"
	// out "xabbo.b7c.io/goearth/shockwave/out"
)

//go:embed all:frontend/dist
var assets embed.FS

var ext = g.NewExt(g.ExtInfo{
	Title:       "Dealer Bets",
	Description: "Standalone betting tracker for G-Earth",
	Version:     "1.0.0",
	Author:      "Dubbo",
})

type Bet struct {
	ID        int     `json:"id"`
	Player    string  `json:"player"`
	Amount    int     `json:"amount"`
	Game      string  `json:"game"`
	Status    string  `json:"status"` // "Pending", "Won", "Lost"
}

type App struct {
	ctx   context.Context
	mu    sync.Mutex
	bets  []Bet
	nextID int
	connected bool
}

func NewApp() *App {
	return &App{
		bets: []Bet{},
		nextID: 1,
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

	// Basic interception examples for a betting app
	ext.Intercept(in.CHAT, in.SHOUT).With(func(e *g.Intercept) {
		// Log chat for debugging or auto-detecting bets if needed
	})

	ext.Run()
}

func (a *App) emitUpdate() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "update", map[string]interface{}{
			"connected": a.connected,
			"bets":      a.bets,
		})
	}
}

// Frontend API
func (a *App) AddBet(player string, amount int, game string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	bet := Bet{
		ID:     a.nextID,
		Player: player,
		Amount: amount,
		Game:   game,
		Status: "Pending",
	}
	a.bets = append(a.bets, bet)
	a.nextID++
	a.emitUpdate()
}

func (a *App) UpdateBetStatus(id int, status string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i, b := range a.bets {
		if b.ID == id {
			a.bets[i].Status = status
			break
		}
	}
	a.emitUpdate()
}

func (a *App) ClearBets() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.bets = []Bet{}
	a.emitUpdate()
}

func (a *App) GetState() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	return map[string]interface{}{
		"connected": a.connected,
		"bets":      a.bets,
	}
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Dealer Bets",
		Width:  800,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.BackgroundColour{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
