package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/jwtly10/go-tunol/pkg/config"
	"github.com/jwtly10/go-tunol/pkg/tunnel"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	server := tunnel.NewServer(logger, &cfg.Server)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			server.Handler().ServeHTTP(w, r)
		} else {
			server.ServeHTTP(w, r)
		}
	})

	port := ":" + cfg.Server.Port

	logger.Info(fmt.Sprintf("Server listening on %s", port))
	if err := http.ListenAndServe(port, nil); err != nil {
		logger.Error("Server error", "error", err)
		os.Exit(1)
	}
}
