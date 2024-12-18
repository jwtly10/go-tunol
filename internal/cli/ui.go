package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gookit/color"
	"github.com/jwtly10/go-tunol/internal/client"
)

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
	// b.WriteString(color.Green.Sprint("   http://localhost:9000 (coming soon)\n\n")) // TODO: Implement this

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
	b.WriteString("\nPress Ctrl+C to quit\n")
	return b.String()
}

func (a *App) startUI() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			clearScreen()
			fmt.Print(a.render())

		}
	}
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}
