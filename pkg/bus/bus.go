// Package bus defines the message types and channel interfaces used by
// PicoClaw's AgentLoop. Reef extends these types for swarm communication.
package bus

// MessageType categorizes the direction of a message.
type MessageType string

const (
	TypeInbound  MessageType = "inbound"
	TypeOutbound MessageType = "outbound"
	TypeSystem   MessageType = "system"
)

// Message is the basic unit of communication in PicoClaw.
type Message struct {
	Type    MessageType     `json:"type"`
	Text    string          `json:"text"`
	Payload map[string]any  `json:"payload,omitempty"`
}

// Channel is the abstraction for inbound/outbound message transport.
// Implementations include local CLI, Telegram, Feishu, and Swarm (Reef).
type Channel interface {
	// Start initializes the channel and begins receiving messages.
	Start() error
	// Stop shuts down the channel gracefully.
	Stop() error
	// Send delivers an outbound message through the channel.
	Send(msg Message) error
	// Receive returns a channel that yields inbound messages.
	Receive() <-chan Message
}
