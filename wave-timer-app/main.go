package main

import (
	"context"
	"embed"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	"xabbo.b7c.io/goearth/shockwave/out"
)

//go:embed all:frontend/dist
var assets embed.FS

var ext = g.NewExt(g.ExtInfo{
	Title:       "Wave Timer",
	Description: "Independent humanized wave timer for G-Earth",
	Version:     "1.0.0",
	Author:      "Dubbo",
})

type WaveConfig struct {
	Minutes      float64 `json:"minutes"`
	Humanize     bool    `json:"humanize"`
	Running      bool    `json:"running"`
	Connected    bool    `json:"connected"`
	LastWaveAt   string  `json:"lastWaveAt,omitempty"`
	NextWaveInMs int64   `json:"nextWaveInMs,omitempty"`
}

type App struct {
	ctx context.Context

	mu sync.Mutex

	running        bool
	humanize       bool
	minutes        float64
	connected      bool
	lastWaveAt     time.Time
	nextWaveAt     time.Time
	stopCh         chan struct{}
	loopGeneration int
}

func NewApp() *App {
	return &App{
		humanize: true,
		minutes:  5,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.runExt()
}

func (a *App) shutdown(context.Context) {
	a.stopLoop()
}

func (a *App) runExt() {
	ext.Run()
}

func (a *App) emitConfigUpdate() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "waveConfigUpdate", a.GetConfig())
}

func (a *App) GetConfig() WaveConfig {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg := WaveConfig{
		Minutes:   a.minutes,
		Humanize:  a.humanize,
		Running:   a.running,
		Connected: a.connected,
	}

	if !a.lastWaveAt.IsZero() {
		cfg.LastWaveAt = a.lastWaveAt.Format(time.RFC3339)
	}

	if a.running && !a.nextWaveAt.IsZero() {
		remaining := time.Until(a.nextWaveAt)
		if remaining > 0 {
			cfg.NextWaveInMs = remaining.Milliseconds()
		}
	}

	return cfg
}

func clampMinutes(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 5
	}
	if v < 0.2 {
		return 0.2
	}
	if v > 240 {
		return 240
	}
	return v
}

func (a *App) SetConfig(minutes float64, humanize bool) WaveConfig {
	a.mu.Lock()
	a.minutes = clampMinutes(minutes)
	a.humanize = humanize
	a.mu.Unlock()
	a.emitConfigUpdate()
	return a.GetConfig()
}

func (a *App) ToggleWave() WaveConfig {
	a.mu.Lock()
	if a.running {
		ch := a.stopCh
		a.running = false
		a.stopCh = nil
		a.loopGeneration++
		a.nextWaveAt = time.Time{}
		a.mu.Unlock()
		if ch != nil {
			close(ch)
		}
		a.emitConfigUpdate()
		return a.GetConfig()
	}

	stopCh := make(chan struct{})
	a.running = true
	a.stopCh = stopCh
	a.loopGeneration++
	generation := a.loopGeneration
	a.mu.Unlock()

	go a.waveLoop(stopCh, generation)
	a.emitConfigUpdate()
	return a.GetConfig()
}

func (a *App) stopLoop() {
	a.mu.Lock()
	ch := a.stopCh
	a.running = false
	a.stopCh = nil
	a.loopGeneration++
	a.nextWaveAt = time.Time{}
	a.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (a *App) computeDelayLocked(rng *rand.Rand) time.Duration {
	base := time.Duration(a.minutes * float64(time.Minute))
	if base < 12*time.Second {
		base = 12 * time.Second
	}

	if !a.humanize {
		return base
	}

	// Humanized timing centered around the selected minutes value.
	// Example: at 5 minutes, sends may happen before or after 5:00.
	jitter := (rng.Float64() * 0.50) - 0.25 // -25% to +25%
	delay := time.Duration(float64(base) * (1 + jitter))

	// Small occasional drift in either direction (negative = earlier, positive = later).
	if rng.Float64() < 0.45 {
		delay += time.Duration(rng.Intn(31)-15) * time.Second
	}

	// Rare larger drift to avoid robotic consistency.
	if rng.Float64() < 0.12 {
		sign := 1
		if rng.Intn(2) == 0 {
			sign = -1
		}
		delay += time.Duration(sign*(45+rng.Intn(46))) * time.Second
	}

	if delay < 12*time.Second {
		delay = 12 * time.Second
	}
	return delay
}

func (a *App) waveLoop(stopCh chan struct{}, generation int) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		a.mu.Lock()
		if !a.running || generation != a.loopGeneration {
			a.mu.Unlock()
			return
		}
		delay := a.computeDelayLocked(rng)
		a.nextWaveAt = time.Now().Add(delay)
		a.mu.Unlock()

		a.emitConfigUpdate()

		timer := time.NewTimer(delay)
		select {
		case <-stopCh:
			timer.Stop()
			return
		case <-timer.C:
			a.sendWave()
		}

		select {
		case <-stopCh:
			return
		case <-ticker.C:
		}
	}
}

func (a *App) sendWave() {
	ext.Send(out.WAVE)

	a.mu.Lock()
	a.lastWaveAt = time.Now()
	a.mu.Unlock()
	a.emitConfigUpdate()
}

func setupExt(a *App) {
	ext.Initialized(func(e g.InitArgs) {
		log.Printf("initialized (connected=%t)", e.Connected)
	})

	ext.Activated(func() {
		log.Printf("activated")
		if a.ctx != nil {
			runtime.WindowShow(a.ctx)
		}
	})

	ext.Connected(func(e g.ConnectArgs) {
		log.Printf("connected (%s:%d)", e.Host, e.Port)
		a.mu.Lock()
		a.connected = true
		a.mu.Unlock()
		a.emitConfigUpdate()
	})

	ext.Disconnected(func() {
		log.Printf("disconnected")
		a.mu.Lock()
		a.connected = false
		a.mu.Unlock()
		a.emitConfigUpdate()
	})
}

func main() {
	app := NewApp()
	setupExt(app)

	err := wails.Run(&options.App{
		Title:             "Wave Timer",
		Width:             360,
		Height:            230,
		MinWidth:          360,
		MaxWidth:          360,
		MinHeight:         230,
		MaxHeight:         230,
		DisableResize:     true,
		StartHidden:       true,
		HideWindowOnClose: true,
		OnStartup:         app.startup,
		OnShutdown:        app.shutdown,
		Bind:              []interface{}{app},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 17, G: 23, B: 30, A: 1},
	})

	if err != nil {
		log.Fatal(err)
	}
}
