package server

import "testing"

func TestExtractTunnelId(t *testing.T) {
	tests := []struct {
		name         string
		urlStr       string
		host         string
		useSubdomain bool
		wantId       string
		wantPath     string
	}{
		{
			name:         "test valid local tunnel with path",
			urlStr:       "/local/abc123/path",
			host:         "localhost:8001",
			useSubdomain: false,
			wantId:       "abc123",
			wantPath:     "/path",
		},
		{
			name:         "test valid local tunnel with no path",
			urlStr:       "/local/abc123",
			host:         "localhost:8001",
			useSubdomain: false,
			wantId:       "abc123",
			wantPath:     "",
		},
		{
			name:         "test valid subdomain tunnel with path",
			urlStr:       "/path",
			host:         "abc123.domain:8001",
			useSubdomain: true,
			wantId:       "abc123",
			wantPath:     "/path",
		},
		{
			name:         "test valid subdomain tunnel with no path",
			urlStr:       "",
			host:         "abc123.domain:8001",
			useSubdomain: true,
			wantId:       "abc123",
			wantPath:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, path, err := extractTunnelIDAndPath(tt.urlStr, tt.host, tt.useSubdomain)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if id != tt.wantId {
				t.Errorf("got id %v, want %v", id, tt.wantId)
			}

			if path != tt.wantPath {
				t.Errorf("got path %v, want %v", path, tt.wantPath)
			}
		})
	}

}
