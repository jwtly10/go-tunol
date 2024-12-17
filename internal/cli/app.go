package cli

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/jwtly10/go-tunol/internal/auth/token"
	"github.com/jwtly10/go-tunol/internal/client"
	"github.com/jwtly10/go-tunol/internal/config"
)

type App struct {
	tunnels    map[string]*tunnelState
	commonLogs []logEntry
	Cfg        *config.ClientConfig
	stats      stats
	mu         sync.Mutex // Protect concurrent access to app state

	logger *slog.Logger
}

type stats struct {
	requestCount    int
	errorCount      int
	avgResponseTime int
}

type tunnelState struct {
	tunnel   client.Tunnel
	isActive bool
	lastErr  error
	uptime   time.Time
}

type logEntry struct {
	timestamp time.Time
	port      int
	method    string
	path      string
	status    int
	duration  int
	error     string
	isError   bool
}

type initError struct {
	port int
	err  error
}

func NewApp(cfg *config.ClientConfig, logger *slog.Logger) *App {
	return &App{
		tunnels:    make(map[string]*tunnelState),
		logger:     logger,
		commonLogs: make([]logEntry, 0),
		Cfg:        cfg,
	}
}

func (a *App) initTunnels() []initError {
	var errs []initError

	for _, port := range a.Cfg.Ports {
		// Create client with event handler
		c := client.NewTunnelManager(a.Cfg, a.logger, func(event client.Event) {
			a.handleEvent(port, event)
		})

		tunnelID := fmt.Sprintf("tunnel_%d", port)
		t, err := c.NewTunnel(port)
		if err != nil {
			a.logger.Error("Error creating tunnel", "port", port, "error", err)
			a.mu.Lock()
			a.tunnels[tunnelID] = &tunnelState{
				isActive: false,
				lastErr:  err,
				uptime:   time.Now(),
			}
			a.mu.Unlock()

			errs = append(errs, initError{port: port, err: err})
			continue
		}

		// At this point the tunnel should be active
		a.mu.Lock()
		a.tunnels[tunnelID] = &tunnelState{
			tunnel:   t,
			isActive: true,
			lastErr:  nil, // Ensure there no error if we get here
			uptime:   time.Now(),
		}
		a.mu.Unlock()
	}

	return errs
}

func (a *App) Start() error {
	if err := ValidateTokenOnServer(a.Cfg, a.logger); err != nil {
		fmt.Printf("Error: Token is no longer valid. Please run 'tunol --login <token>' again. (Reason: %v)\n", err)
		os.Exit(1)
	}

	if errs := a.initTunnels(); len(errs) != 0 {
		fmt.Println("Error initializing tunnels:")
		for _, err := range errs {
			fmt.Printf("  Port %d: %v\n", err.port, err.err)
		}
		os.Exit(1)
	}

	go a.startUI()
	return nil
}

// Login logs the user in with the current application configuration
func (a *App) Login() error {
	if err := ValidateTokenOnServer(a.Cfg, a.logger); err != nil {
		return fmt.Errorf("failed to validate token: %w", err)
	}

	store, err := token.NewTokenStore()
	if err != nil {
		return fmt.Errorf("failed to create token store: %w", err)
	}

	if err := store.StoreToken(a.Cfg.Token); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	a.logger.Info("Login successful")
	fmt.Println("Login successful. You can now tunnel ports with 'tunol [--port <port>]'")
	return nil
}
