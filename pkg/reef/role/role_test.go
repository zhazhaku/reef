package role

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "coder.yaml")
	data := []byte(`
name: coder
skills:
  - github
  - docker
  - go
system_prompt: You are a senior Go developer.
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Name != "coder" {
		t.Errorf("name = %s, want coder", cfg.Name)
	}
	if len(cfg.Skills) != 3 {
		t.Errorf("skills = %v, want 3", cfg.Skills)
	}
	if cfg.SystemPrompt == "" {
		t.Error("expected system_prompt to be set")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/role.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoad_MissingName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, []byte("skills:\n  - go\n"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestLoad_MissingSkills(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, []byte("name: empty\n"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for missing skills")
	}
}

func TestConfig_Validate(t *testing.T) {
	cfg := &Config{
		Name:   "coder",
		Skills: []string{"go", "docker"},
	}
	available := map[string]struct{}{
		"go":     {},
		"docker": {},
		"github": {},
	}
	if err := cfg.Validate(available); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	cfg.Skills = append(cfg.Skills, "kubernetes")
	if err := cfg.Validate(available); err == nil {
		t.Error("expected error for missing skill")
	}
}
