package server

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
)

// generateID generates a unique ID
func generateID() string {
	// TODO: This is a nicer ID that UUID, but collisons are possible. We need to consider this in future
	// if this was to be more than a personal application
	const charset = "abcdefghjkmnpqrstuvwxyz23456789"
	length := 8
	id := make([]byte, length)

	for i := range id {
		randByte := make([]byte, 1)
		if _, err := rand.Read(randByte); err != nil {
			panic(err)
		}
		id[i] = charset[randByte[0]%byte(len(charset))]
	}

	return string(id)
}

// extractTunnelIDAndPath extracts the tunnel ID and the remaining path from a URL
func extractTunnelIDAndPath(urlStr string, host string, useSubdomain bool) (tunnelID string, remainingPath string, err error) {
	if useSubdomain {
		parts := strings.Split(host, ".")
		if len(parts) < 2 {
			return "", "", fmt.Errorf("invalid host: %s", host)
		}
		tunnelID = parts[0]
		remainingPath = urlStr // Use full URL path
		return tunnelID, remainingPath, nil
	}

	// Local routing
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", "", err
	}

	segments := strings.Split(strings.TrimPrefix(parsedURL.Path, "/"), "/")
	// We only need 2 segments: "local" and the tunnelID
	if len(segments) < 2 || segments[0] != "local" {
		return "", "", fmt.Errorf("invalid local tunnel path format")
	}

	tunnelID = segments[1]
	if tunnelID == "" {
		return "", "", fmt.Errorf("empty tunnel_id")
	}

	// Add any additional path segments
	if len(segments) > 2 {
		remainingPath = "/" + strings.Join(segments[2:], "/")
	}

	return tunnelID, remainingPath, nil
}
