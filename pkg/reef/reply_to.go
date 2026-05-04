// Package reef defines reply-to context for result routing.

package reef

import "encoding/json"

// ReplyToContext captures the source of a task request so results can be
// routed back to the originating channel, chat, and user.
type ReplyToContext struct {
	Channel   string `json:"channel"`    // e.g. "feishu", "telegram"
	ChatID    string `json:"chat_id"`    // e.g. "oc_xxx"
	UserID    string `json:"user_id"`    // e.g. "ou_xxx"
	MessageID string `json:"message_id"` // optional: reply to specific message
	ThreadID  string `json:"thread_id"`  // optional: thread root ID
}

// IsZero returns true if the ReplyToContext has no routing information.
func (r *ReplyToContext) IsZero() bool {
	return r == nil || (r.Channel == "" && r.ChatID == "" && r.UserID == "")
}

// Bytes serialises to JSON bytes.
func (r *ReplyToContext) Bytes() ([]byte, error) {
	return json.Marshal(r)
}

// ParseReplyTo deserialises JSON bytes into a ReplyToContext.
func ParseReplyTo(data []byte) (*ReplyToContext, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var r ReplyToContext
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.IsZero() {
		return nil, nil
	}
	return &r, nil
}
