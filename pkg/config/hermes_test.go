package config

import "testing"

func TestHermesConfig_HermesMode(t *testing.T) {
	tests := []struct {
		cfg  HermesConfig
		want string
	}{
		{HermesConfig{}, "full"},
		{HermesConfig{Mode: "full"}, "full"},
		{HermesConfig{Mode: "coordinator"}, "coordinator"},
		{HermesConfig{Mode: "executor"}, "executor"},
	}

	for _, tt := range tests {
		if got := tt.cfg.HermesMode(); got != tt.want {
			t.Errorf("HermesMode() = %q, want %q", got, tt.want)
		}
	}
}

func TestHermesConfig_IsCoordinator(t *testing.T) {
	if (HermesConfig{}).IsCoordinator() {
		t.Error("empty config should not be coordinator")
	}
	if !(HermesConfig{Mode: "coordinator"}).IsCoordinator() {
		t.Error("coordinator mode should be coordinator")
	}
}

func TestHermesConfig_IsExecutor(t *testing.T) {
	if !(HermesConfig{Mode: "executor"}).IsExecutor() {
		t.Error("executor mode should be executor")
	}
}

func TestHermesConfig_IsFull(t *testing.T) {
	if !(HermesConfig{}).IsFull() {
		t.Error("empty config should be full")
	}
	if !(HermesConfig{Mode: "full"}).IsFull() {
		t.Error("full mode should be full")
	}
	if (HermesConfig{Mode: "coordinator"}).IsFull() {
		t.Error("coordinator mode should not be full")
	}
}
