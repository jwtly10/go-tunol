package client

import "time"

type EventType string

const (
	EventTypeRequest EventType = "request"
	EventTypeError   EventType = "error"
)

type RequestEvent struct {
	TunnelID  string
	Method    string
	Path      string
	Status    int
	Duration  time.Duration
	Error     string
	Timestamp time.Time

	// ConnectionFailed is set to true if the manager lost connection to the server
	ConnectionFailed bool
}

type ErrorEvent struct {
	Error string `json:"error"`
}

type Event struct {
	Type    EventType   `json:"type"`
	Payload interface{} `json:"payload"`
}
