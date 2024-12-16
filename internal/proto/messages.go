package proto

type MessageType string

const (
	MessageTypePing MessageType = "ping"
	MessageTypePong MessageType = "pong"

	MessageTypeTunnelReq  MessageType = "tunnel_req"
	MessageTypeTunnelResp MessageType = "tunnel_resp"

	MessageTypeHTTPRequest  MessageType = "http_request"
	MessageTypeHTTPResponse MessageType = "http_response"

	MessageTypeError MessageType = "error"
)

type Message struct {
	Type    MessageType `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

type TunnelRequest struct {
	// LocalPort is the local port to tunnel and expose to the public internet
	LocalPort int `json:"local_port"`
}

type TunnelResponse struct {
	// URL is the public URL of the tunnel to the local port
	URL string `json:"url"`
}
