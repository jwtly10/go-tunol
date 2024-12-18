package server

import (
	"net/http"
	"strings"
)

func (th *TunnelHandler) extractToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if token != "" {
		return strings.TrimPrefix(token, "Bearer ")
	}

	return ""
}
