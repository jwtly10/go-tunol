package main

import (
	"flag"
	"fmt"
	"github.com/gookit/color"
	"github.com/jwtly10/go-tunol/internal/auth/token"
	"github.com/jwtly10/go-tunol/internal/client"
	"github.com/jwtly10/go-tunol/internal/config"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"sync"
	"time"
)

const (
	// Default prod URL
	defaultServerURL = "https://tunol.dev"

	serverURLEnv = "TUNOL_SERVER_URL"
)

func setupCLILogger() *slog.Logger {
	// Logs will be saved at ~/.tunol/logs/tunol-cli.log
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	logsDir := filepath.Join(homeDir, ".tunol", "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		fmt.Printf("Error creating logs directory: %v\n", err)
		os.Exit(1)
	}

	logFile := filepath.Join(logsDir, "tunol-cli.log")
	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("Error opening log file: %v\n", err)
		os.Exit(1)
	}

	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewTextHandler(f, opts)
	logger := slog.New(handler)

	logger.Info("tunol CLI started", "version", "0.1.0")

	return logger
}

type portFlags []int

func (f *portFlags) String() string {
	return fmt.Sprint(*f)
}

func (f *portFlags) Set(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid port number: %v", err)
	}
	*f = append(*f, port)
	return nil
}

type App struct {
	tunnels    map[string]*tunnelState
	ports      []int
	logger     *slog.Logger
	commonLogs []logEntry
	stats      stats
	mu         sync.Mutex // Protect concurrent access to app state

	cfg *config.ClientConfig
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

func NewApp(ports []int, logger *slog.Logger, cfg *config.ClientConfig) *App {
	return &App{
		tunnels:    make(map[string]*tunnelState),
		ports:      ports,
		logger:     logger,
		commonLogs: make([]logEntry, 0),
		cfg:        cfg,
	}
}

type initError struct {
	port int
	err  error
}

func (a *App) initTunnels() []initError {
	var errs []initError

	for _, port := range a.ports {
		// Create client with event handler
		c := client.NewTunnelManager(a.cfg, a.logger, func(event client.Event) {
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

func (a *App) handleEvent(port int, event client.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch event.Type {
	case client.EventTypeError:
		// If the connection has failed due to auth, log for user and kill CLI
		// TODO: I guess the only time this would happen is it the tunnel has already been created and THEN the token expires....
		// TODO: Handle in future, for now just log and close
		fmt.Printf("There was an error during the tunnel session: %v\n", event.Payload.(client.ErrorEvent).Error)
		os.Exit(1)
	case client.EventTypeRequest:
		// If the connection has failed (but not due to auth, some other http issue), log for user and kill CLI
		if event.Payload.(client.RequestEvent).ConnectionFailed {
			a.logger.Error("Connection to tunol server failed, shutting down", "port", port)
			tunnelId := fmt.Sprintf("tunnel_%d", port)
			if state, exists := a.tunnels[tunnelId]; exists {
				state.isActive = false
				state.lastErr = fmt.Errorf("connection to tunol server failed")
			}

			// Close all tunnels
			for _, state := range a.tunnels {
				if state.isActive {
					state.tunnel.Close()
				}
			}

			// The event contains an error message, so we log it
			if event.Payload.(client.RequestEvent).Error != "" {
				fmt.Println("Shutting down due to error:", event.Payload.(client.RequestEvent).Error)
			}

			os.Exit(1)
		}

		// Else we handle the request event

		// Update stats
		a.stats.requestCount++
		if event.Payload.(client.RequestEvent).Status >= 500 || event.Payload.(client.RequestEvent).Error != "" {
			a.stats.errorCount++
		}

		// Update average response time
		duration := int(event.Payload.(client.RequestEvent).Duration.Milliseconds())
		a.stats.avgResponseTime = (a.stats.avgResponseTime*a.stats.requestCount + duration) / (a.stats.requestCount + 1)

		// Add log entry
		a.commonLogs = append(a.commonLogs, logEntry{
			timestamp: time.Now(),
			port:      port,
			method:    event.Payload.(client.RequestEvent).Method,
			path:      event.Payload.(client.RequestEvent).Path,
			status:    event.Payload.(client.RequestEvent).Status,
			duration:  duration,
			error:     event.Payload.(client.RequestEvent).Error,
			isError:   event.Payload.(client.RequestEvent).Status >= 500 || event.Payload.(client.RequestEvent).Error != "",
		})

		// Keep only last 100 logs
		if len(a.commonLogs) > 100 {
			a.commonLogs = a.commonLogs[1:]
		}
	}
}

func (a *App) render() string {
	var b strings.Builder

	// Header
	b.WriteString(color.Bold.Sprintf(" go-tunol dashboard%51s\n", time.Now().Format("15:04:05")))
	b.WriteString("══════════════════════════════════════════════════════\n")

	// Tunnels Section
	b.WriteString(color.Bold.Sprint("📡 TUNNELS\n"))
	// Nice to have - sort the urls so they don't change on render (maps pseudo random order)
	var tunnelIDs []string
	for id := range a.tunnels {
		tunnelIDs = append(tunnelIDs, id)
	}
	sort.Strings(tunnelIDs)

	for _, id := range tunnelIDs {
		state := a.tunnels[id]
		if state.isActive {
			uptime := time.Since(state.uptime).Round(time.Second)
			tunnelLine := fmt.Sprintf("   %s ➔ %s (⬆️ %s)",
				state.tunnel.URL(),
				"localhost:"+strconv.Itoa(state.tunnel.LocalPort()),
				uptime)
			b.WriteString(tunnelLine + "\n")
		} else {
			errLine := fmt.Sprintf("   [%s] ➔ (❌ %s)",
				id,
				state.lastErr)
			b.WriteString(errLine + "\n")
		}
	}
	b.WriteString("\n")

	// Admin Tool Section
	b.WriteString(color.Bold.Sprint("🔗 ADMIN TOOL\n"))
	b.WriteString(color.Green.Sprint("   http://localhost:9000\n\n")) // TODO: Implement this frontend admin dash

	// Stats Section
	b.WriteString(color.Bold.Sprint("📊 STATS (last 60s)\n"))
	var successRate float64
	if a.stats.requestCount > 0 {
		successRate = 100.0 * float64(a.stats.requestCount-a.stats.errorCount) / float64(a.stats.requestCount)
	}
	statsLine := fmt.Sprintf("   %d requests • %d errors • %.1f%% success rate",
		a.stats.requestCount,
		a.stats.errorCount,
		successRate)
	b.WriteString(statsLine + "\n")
	b.WriteString(fmt.Sprintf("   Average response time: %dms\n\n", a.stats.avgResponseTime))

	// Traffic Section
	b.WriteString(color.Bold.Sprint("LIVE TRAFFIC (newest first)\n"))
	b.WriteString("────────────────────────────────────────────────────\n")

	// Display last 6 log entries (or all if less than 6)
	start := len(a.commonLogs)
	if start > 6 {
		start = 6
	}
	for i := len(a.commonLogs) - 1; i >= len(a.commonLogs)-start && i >= 0; i-- {
		log := a.commonLogs[i]
		statusSymbol := color.Green.Sprint("✓")

		// Update status symbol depending on req
		if log.isError {
			statusSymbol = color.Red.Sprint("✗")
		} else if log.status >= 400 {
			statusSymbol = color.Yellow.Sprint("!")
		}

		// If path is empty, set it to root
		if log.path == "" {
			log.path = "/"
		}

		logLine := fmt.Sprintf("   [:%d] %d %s    %s    %dms %s",
			log.port,
			log.status,
			log.method,
			log.path,
			log.duration,
			statusSymbol)

		// TODO: There is an issue with setting the raw body error msg
		// Theres an additional \n being rendered, for now will just remove
		//if log.isError && log.error != "" {
		//	logLine = fmt.Sprintf("%s ERROR: %s", logLine, log.error)
		//}

		b.WriteString(logLine)
		b.Write([]byte("\n"))
	}

	// Footer
	b.WriteString("\nPress Ctrl+C to quit • Press 'c' to clear logs\n")
	return b.String()
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func (a *App) loginCommand(t string) error {
	if err := validateTokenOnServer(t, a.cfg.ServerURL); err != nil {
		return fmt.Errorf("failed to validate token: %w", err)
	}

	store, err := token.NewTokenStore()
	if err != nil {
		return fmt.Errorf("failed to create token store: %w", err)
	}

	if err := store.StoreToken(t); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	a.logger.Info("Login successful")
	fmt.Println("Login successful. You can now tunnel ports with 'tunol [--port <port>]'")
	return nil
}

func validateTokenOnServer(token string, serverHost string) error {
	// Validate token format (should be two UUIDs joined with a hyphen)
	// Expected format: UUID-UUID (where each UUID is 36 chars)
	if len(token) != 73 { // 36 + 1 + 36
		return fmt.Errorf("invalid token format: incorrect length")
	}

	c := &http.Client{}
	req, err := http.NewRequest("GET", serverHost+"/auth/validate", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("failed to validate token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid token")
	}

	return nil
}

func main() {
	var (
		ports      portFlags
		loginToken string
		serverURL  string
	)

	flag.Var(&ports, "port", "Port to tunnel (can be specified multiple times)")
	flag.StringVar(&loginToken, "login", "", "Login with the provided token")
	flag.StringVar(&serverURL, "server", "", "Server URL (defaults to TUNOL_SERVER_URL env var or https://tunol.dev)")
	flag.Parse()
	logger := setupCLILogger()

	if serverURL == "" {
		serverURL = os.Getenv(serverURLEnv)
		if serverURL == "" {
			serverURL = defaultServerURL
		}
	}

	cfg := &config.ClientConfig{
		ServerURL: serverURL,
	}
	app := NewApp([]int(ports), logger, cfg)

	if loginToken != "" {
		if err := app.loginCommand(loginToken); err != nil {
			fmt.Printf("Login failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(ports) == 0 {
		fmt.Println("Usage:")
		fmt.Println("  tunol --port <port> [--port <port>...]")
		fmt.Println("  tunol --login <token>")
		os.Exit(1)
	}

	if len(ports) > 5 {
		fmt.Println("Error: Maximum of 5 ports can be tunneled at once")
		os.Exit(1)
	}

	// Check if logged in when running tunnel commands
	store, err := token.NewTokenStore()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	token, err := store.GetToken()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if token == "" {
		fmt.Println("Error: Not logged in. Please run 'tunol --login <token>' first")
		os.Exit(1)
	}

	// Now we should validate the token before running the tunnels, just to give a nice error message
	if err := validateTokenOnServer(token, cfg.ServerURL); err != nil {
		fmt.Printf("Error: Token is no longer valid. Please run 'tunol --login <token>' again. (Reason: %v)\n", err)
		os.Exit(1)
	}

	// Now we have the token, we should set the app config to use it
	app.cfg.Token = token

	// Initialize tunnels
	// If we cant init the tunnels correctly we stop running the cli
	if errs := app.initTunnels(); len(errs) != 0 {
		fmt.Println("Error initializing tunnels:")
		for _, err := range errs {
			fmt.Printf("  Port %d: %v\n", err.port, err.err)
		}
		os.Exit(1)
	}

	// Setup signal handling for clean shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	// Render loop
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			clearScreen()
			fmt.Print(app.render())
		case <-sigChan:
			fmt.Println("\nShutting down...")
			return
		}
	}
}
