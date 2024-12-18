package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/jwtly10/go-tunol/internal/auth/token"
	"github.com/jwtly10/go-tunol/internal/cli"
)

// This is the main entry point for the CLI client application

// TODO: We should have this limit on the server side, instead of here...
const maxConcurrentTunnels = 5

func main() {
	cfg := cli.ParseFlags()
	logger := cli.SetupLogger()
	app := cli.NewApp(cfg, logger)

	// cfg.Token is only set here if is the user has run the login command
	if cfg.Token != "" {
		// If the user has run the login command, we should run the Login flow
		if err := app.Login(); err != nil {
			fmt.Printf("Login failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := validatePorts(cfg.Ports); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	t, err := getAndValidateToken()
	if err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}

	// Now we have the token, we should set the app config to use it
	app.Cfg.Token = t

	err = app.Start()
	if err != nil {
		fmt.Printf("Error starting tunnels: %v\n", err)
		os.Exit(1)
	}

	waitForShutdown()
}

func validatePorts(ports []int) error {
	if len(ports) == 0 {
		return fmt.Errorf("Usage:\n  tunol --port <port> [--port <port>...]\n  tunol --login <token>")
	}
	if len(ports) > maxConcurrentTunnels {
		return fmt.Errorf("Error: Maximum of %d ports can be tunneled at once", maxConcurrentTunnels)
	}
	return nil
}

func getAndValidateToken() (string, error) {
	store, err := token.NewTokenStore()
	if err != nil {
		return "", err
	}

	token, err := store.GetToken()
	if err != nil {
		return "", err
	}

	if token == "" {
		return "", fmt.Errorf("not logged in. Please run 'tunol --login <token>' first")
	}

	return token, nil
}

func waitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan
	fmt.Println("\nShutting down...")
}
