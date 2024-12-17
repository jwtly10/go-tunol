package cli

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jwtly10/go-tunol/internal/config"
)

func ValidateTokenOnServer(cfg *config.ClientConfig, logger *slog.Logger) error {
	// Validate token format (should be two UUIDs joined with a hyphen)
	// Expected format: UUID-UUID (where each UUID is 36 chars)
	if len(cfg.Token) != 73 { // 36 + 1 + 36
		return fmt.Errorf("invalid token format: incorrect length")
	}

	c := &http.Client{}
	req, err := http.NewRequest("GET", cfg.ServerURL+"/auth/validate", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

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
