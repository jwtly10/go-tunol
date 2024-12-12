package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

func HashToken(token string) string {
	h := sha256.New()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}
