package agent

import (
	"context"
	"testing"

	"github.com/zhazhaku/reef/pkg/providers"
)

func TestCNPContextManager_Register(t *testing.T) {
	f, ok := lookupContextManager("cnp")
	if !ok {
		t.Fatal("cnp context manager not registered")
	}
	if f == nil {
		t.Fatal("factory is nil")
	}
}

func TestCNPContextManager_New(t *testing.T) {
	cm, err := NewCNPContextManager(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cnp, ok := cm.(*CNPContextManager)
	if !ok {
		t.Fatal("wrong type")
	}
	if cnp.SessionCount() != 0 {
		t.Errorf("sessions = %d", cnp.SessionCount())
	}
}

func TestCNPContextManager_IngestCreatesSession(t *testing.T) {
	cm, _ := NewCNPContextManager(nil, nil)

	err := cm.Ingest(context.Background(), &IngestRequest{
		SessionKey: "s1",
		Message: providers.Message{
			Role:    "user",
			Content: "hello",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cnp := cm.(*CNPContextManager)
	if cnp.SessionCount() != 1 {
		t.Errorf("sessions = %d", cnp.SessionCount())
	}
}

func TestCNPContextManager_Assemble(t *testing.T) {
	cm, _ := NewCNPContextManager(nil, nil)

	cm.Ingest(context.Background(), &IngestRequest{
		SessionKey: "s1",
		Message:    providers.Message{Role: "user", Content: "hello"},
	})
	cm.Ingest(context.Background(), &IngestRequest{
		SessionKey: "s1",
		Message:    providers.Message{Role: "assistant", Content: "hi there"},
	})

	resp, err := cm.Assemble(context.Background(), &AssembleRequest{
		SessionKey: "s1",
		Budget:     4096,
		MaxTokens:  1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.History) != 2 {
		t.Fatalf("history = %d, want 2", len(resp.History))
	}
	if resp.History[0].Role != "user" {
		t.Errorf("msg[0].Role = %s", resp.History[0].Role)
	}
}

func TestCNPContextManager_Assemble_WithSystemPrompt(t *testing.T) {
	cm, _ := NewCNPContextManager(nil, nil)

	// Manually set immutable + task on a session
	cnp := cm.(*CNPContextManager)
	s := cnp.getSession("s1")
	s.layers.SetImmutable("You are a helpful coder", "coder", nil, nil)
	s.layers.SetTask("Fix the bug in auth.go", nil)

	resp, err := cm.Assemble(context.Background(), &AssembleRequest{
		SessionKey: "s1",
		Budget:     4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	// System prompt + task instruction
	if len(resp.History) != 2 {
		t.Fatalf("history = %d, want 2 (system + task)", len(resp.History))
	}
}

func TestCNPContextManager_Assemble_CorruptionWarning(t *testing.T) {
	cm, _ := NewCNPContextManager(nil, nil)

	cnp := cm.(*CNPContextManager)
	s := cnp.getSession("s1")

	// Feed 5 identical tool calls → loop detected
	for i := 0; i < 5; i++ {
		s.guard.FeedRound("exec", "ok", "ok")
		s.layers.AppendRound(WorkingRound{Round: i + 1, Call: "exec", Output: "ok"})
	}

	resp, err := cm.Assemble(context.Background(), &AssembleRequest{
		SessionKey: "s1",
		Budget:     4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Summary == "" {
		t.Error("expected corruption summary")
	}
	if resp.Summary[0:11] != "[Corruption" {
		t.Errorf("summary = %s", resp.Summary)
	}
}

func TestCNPContextManager_Compact(t *testing.T) {
	cm, _ := NewCNPContextManager(nil, nil)

	// Should not error on compact
	err := cm.Compact(context.Background(), &CompactRequest{
		SessionKey: "s1",
		Reason:     "summarize",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCNPContextManager_Clear(t *testing.T) {
	cm, _ := NewCNPContextManager(nil, nil)

	cm.Ingest(context.Background(), &IngestRequest{
		SessionKey: "s1",
		Message:    providers.Message{Role: "user", Content: "hi"},
	})

	cnp := cm.(*CNPContextManager)
	if cnp.SessionCount() != 1 {
		t.Fatal("session not created")
	}

	cm.Clear(context.Background(), "s1")
	if cnp.SessionCount() != 0 {
		t.Errorf("sessions = %d after clear", cnp.SessionCount())
	}
}

func TestCNPContextManager_SessionIsolation(t *testing.T) {
	cm, _ := NewCNPContextManager(nil, nil)

	cm.Ingest(context.Background(), &IngestRequest{
		SessionKey: "a", Message: providers.Message{Role: "user", Content: "task A"},
	})
	cm.Ingest(context.Background(), &IngestRequest{
		SessionKey: "b", Message: providers.Message{Role: "user", Content: "task B"},
	})

	cnp := cm.(*CNPContextManager)
	if cnp.SessionCount() != 2 {
		t.Fatalf("sessions = %d, want 2", cnp.SessionCount())
	}

	respA, _ := cm.Assemble(context.Background(), &AssembleRequest{SessionKey: "a", Budget: 4096})
	respB, _ := cm.Assemble(context.Background(), &AssembleRequest{SessionKey: "b", Budget: 4096})

	if respA.History[0].Content != "task A" {
		t.Errorf("A content = %s", respA.History[0].Content)
	}
	if respB.History[0].Content != "task B" {
		t.Errorf("B content = %s", respB.History[0].Content)
	}
}

func TestCNPContextManager_RegisterDuplicate(t *testing.T) {
	err := RegisterContextManager("cnp", NewCNPContextManager)
	if err == nil {
		t.Error("expected duplicate registration error")
	}
}

func TestCNPContextManager_Factory(t *testing.T) {
	cm, err := NewCNPContextManager([]byte(`{"max_tokens": 2048}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	cnp := cm.(*CNPContextManager)
	if cnp.cfg.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d", cnp.cfg.MaxTokens)
	}
}
