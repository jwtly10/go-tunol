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
			name:        "test production style url",
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

func TestClientConfigWebSocketURL(t *testing.T) {
	tests := []struct {
		name             string
		serverURL        string
		wantWebSocketURL string
	}{
		{
			name:             "test client config with localserver url",
			serverURL:        "http://localhost:8001",
			wantWebSocketURL: "ws://localhost:8001/tunnel",
		},
		{
			name:             "test client config with production server url",
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
