package reef

import (
	"testing"
)

func TestReplyToContext_IsZero(t *testing.T) {
	r := &ReplyToContext{}
	if !r.IsZero() {
		t.Error("empty ReplyToContext should be zero")
	}

	r.Channel = "feishu"
	if r.IsZero() {
		t.Error("ReplyToContext with channel should not be zero")
	}

	var nilR *ReplyToContext
	if !nilR.IsZero() {
		t.Error("nil ReplyToContext should be zero")
	}
}

func TestReplyToContext_Bytes(t *testing.T) {
	r := &ReplyToContext{
		Channel:   "feishu",
		ChatID:    "oc_test",
		UserID:    "ou_test",
		MessageID: "msg_1",
	}
	data, err := r.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if len(data) == 0 {
		t.Error("serialised data should not be empty")
	}
}

func TestParseReplyTo(t *testing.T) {
	data := []byte(`{"channel":"feishu","chat_id":"oc_test","user_id":"ou_test"}`)
	r, err := ParseReplyTo(data)
	if err != nil {
		t.Fatalf("ParseReplyTo: %v", err)
	}
	if r.Channel != "feishu" {
		t.Errorf("channel = %s, expected feishu", r.Channel)
	}
	if r.ChatID != "oc_test" {
		t.Errorf("chat_id = %s", r.ChatID)
	}

	r2, err := ParseReplyTo([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParseReplyTo(empty): %v", err)
	}
	if r2 != nil {
		t.Error("empty ReplyTo should be nil")
	}

	r3, err := ParseReplyTo(nil)
	if err != nil {
		t.Fatalf("ParseReplyTo(nil): %v", err)
	}
	if r3 != nil {
		t.Error("nil ReplyTo should be nil")
	}
}
