package config

import "testing"

func TestServerConfigHTTPURL(t *testing.T) {
	tests := []struct {
		name        string
		baseUrl     string
		port        string
		wantHTTPURL string
	}{
		{
			name:        "test localhost url set during development",
			baseUrl:     "http://localhost",
			port:        "8001",
			wantHTTPURL: "http://localhost:8001",
		},
		{
			name:        "test production style url does not change",
			baseUrl:     "https://tunol.dev",
			port:        "8001",
			wantHTTPURL: "https://tunol.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverConfig := ServerConfig{
				BaseURL: tt.baseUrl,
				Port:    tt.port,
			}
			if got := serverConfig.HTTPURL(); got != tt.wantHTTPURL {
				t.Errorf("HTTPURL() = %v, want %v", got, tt.wantHTTPURL)
			}
		})
	}
}

func TestServerConfigSubdomainURL(t *testing.T) {
	tests := []struct {
		name          string
		baseUrl       string
		useSubdomains bool
		port          string
		id            string
		wantURL       string
	}{
		{
			name:          "test localhost 'subdomain' with port",
			baseUrl:       "http://localhost",
			useSubdomains: false,
			port:          "8001",
			id:            "abc123",
			wantURL:       "http://localhost:8001/local/abc123",
		},
		{
			name:          "test localhost 'subdomain' without port",
			baseUrl:       "http://localhost",
			port:          "",
			useSubdomains: false,
			id:            "abc123",
			wantURL:       "http://localhost/local/abc123",
		},
		{
			name:          "test production subdomain",
			baseUrl:       "https://tunol.dev",
			port:          "8001", // port should be ignored for prod https URLs
			useSubdomains: true,
			id:            "abc123",
			wantURL:       "https://abc123.tunol.dev",
		},
		{
			name:          "test production subdomain",
			baseUrl:       "https://tunol.dev",
			port:          "8001", // port should be ignored for prod https URLs
			useSubdomains: false,
			id:            "abc123",
			wantURL:       "https://tunol.dev/local/abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverConfig := ServerConfig{
				BaseURL:       tt.baseUrl,
				Port:          tt.port,
				UseSubdomains: tt.useSubdomains,
			}
			if got := serverConfig.SubdomainURL(tt.id); got != tt.wantURL {
				t.Errorf("SubdomainURL() = %v, want %v", got, tt.wantURL)
			}
		})
	}
}

func TestClientConfigWebSocketURL(t *testing.T) {
	tests := []struct {
		name             string
		serverURL        string
		wantWebSocketURL string
	}{
		{
			name:             "test client ws url with localhost server url",
			serverURL:        "http://localhost:8001",
			wantWebSocketURL: "ws://localhost:8001/tunnel",
		},
		{
			name:             "test client ws url with production server url",
			serverURL:        "https://tunol.dev",
			wantWebSocketURL: "wss://tunol.dev/tunnel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientConfig := ClientConfig{
				ServerURL: tt.serverURL,
			}
			if got := clientConfig.WebSocketURL(); got != tt.wantWebSocketURL {
				t.Errorf("WebSocketURL() = %v, want %v", got, tt.wantWebSocketURL)
			}
		})
	}

}
