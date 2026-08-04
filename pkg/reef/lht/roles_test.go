package lht

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zhazhaku/reef/pkg/reef/role"
)

// tempRolesDir creates a temporary roles directory with the three LHT YAMLs.
func tempRolesDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "lht-roles-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	roles := map[string]string{
		"lht-gen.yaml": `name: lht-gen
skills:
  - go
  - github
system_prompt: |
  Generator role for testing.
`,
		"lht-eval.yaml": `name: lht-eval
skills:
  - go
system_prompt: |
  Evaluator role for testing.
`,
		"lht-rev.yaml": `name: lht-rev
skills:
  - go
  - summarize
system_prompt: |
  Reviewer role for testing.
`,
	}

	for name, content := range roles {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	return dir
}

// ────────────────────────────────────────────────────────────
// mockCommandRunner — captures command executions for testing.
// ────────────────────────────────────────────────────────────

type mockCommandRunner struct {
	calls    []commandCall
	outputs  map[string]string // key: "name arg1 arg2" → output
	failOn   map[string]error
}

type commandCall struct {
	Name string
	Args []string
}

func (m *mockCommandRunner) Run(name string, args ...string) ([]byte, error) {
	key := name
	for _, a := range args {
		key += " " + a
	}
	m.calls = append(m.calls, commandCall{Name: name, Args: append([]string{}, args...)})

	if err, ok := m.failOn[key]; ok {
		return nil, err
	}
	if out, ok := m.outputs[key]; ok {
		return []byte(out), nil
	}
	return []byte("OK"), nil
}

func newMockRunner() *mockCommandRunner {
	return &mockCommandRunner{
		outputs: make(map[string]string),
		failOn:  make(map[string]error),
	}
}

// ────────────────────────────────────────────────────────────
// T4A.1.1 — YAML 角色定义加载
// ────────────────────────────────────────────────────────────

func TestRoleYAMLLoading(t *testing.T) {
	dir := tempRolesDir(t)

	for _, name := range []string{"lht-gen", "lht-eval", "lht-rev"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".yaml")
			cfg, err := role.Load(path)
			if err != nil {
				t.Fatalf("role.Load(%s): %v", path, err)
			}
			if cfg.Name != name {
				t.Errorf("config.Name = %q, want %q", cfg.Name, name)
			}
			if len(cfg.Skills) == 0 {
				t.Errorf("%s: expected at least 1 skill, got 0", name)
			}
			if cfg.SystemPrompt == "" {
				t.Errorf("%s: system_prompt is empty", name)
			}
		})
	}
}

// T4A.1.2 — RoleManager.EnsureRole 创建 roleClients 映射项
func TestRoleManagerEnsureRole(t *testing.T) {
	dir := tempRolesDir(t)
	rm := NewRoleManager(dir)

	rc, err := rm.EnsureRole("lht-gen")
	if err != nil {
		t.Fatalf("EnsureRole(lht-gen): %v", err)
	}
	if rc == nil {
		t.Fatal("EnsureRole returned nil")
	}
	if rc.Name != "lht-gen" {
		t.Errorf("rc.Name = %q, want lht-gen", rc.Name)
	}
	if rc.Config == nil {
		t.Fatal("RoleClient.Config is nil")
	}
	if rc.Config.Name != "lht-gen" {
		t.Errorf("Config.Name = %q", rc.Config.Name)
	}

	// Second call should return the same instance (no re-load).
	rc2, err := rm.EnsureRole("lht-gen")
	if err != nil {
		t.Fatalf("second EnsureRole(lht-gen): %v", err)
	}
	if rc2 != rc {
		t.Error("second EnsureRole should return the same instance")
	}
}

// T4A.1.3 — RoleManager 隔离性：不同 role 映射独立
func TestRoleManagerIsolation(t *testing.T) {
	dir := tempRolesDir(t)
	rm := NewRoleManager(dir)

	gen, err := rm.EnsureRole("lht-gen")
	if err != nil {
		t.Fatalf("EnsureRole(lht-gen): %v", err)
	}
	eval, err := rm.EnsureRole("lht-eval")
	if err != nil {
		t.Fatalf("EnsureRole(lht-eval): %v", err)
	}
	rev, err := rm.EnsureRole("lht-rev")
	if err != nil {
		t.Fatalf("EnsureRole(lht-rev): %v", err)
	}

	if gen == eval || eval == rev || gen == rev {
		t.Error("three roles must be independent instances")
	}
	if gen.Config.Name != "lht-gen" {
		t.Errorf("gen name = %q", gen.Config.Name)
	}
	if eval.Config.Name != "lht-eval" {
		t.Errorf("eval name = %q", eval.Config.Name)
	}
	if rev.Config.Name != "lht-rev" {
		t.Errorf("rev name = %q", rev.Config.Name)
	}
}

// T4A.1.4 — RoleManager.Release 释放
func TestRoleManagerRelease(t *testing.T) {
	dir := tempRolesDir(t)
	rm := NewRoleManager(dir)

	_, err := rm.EnsureRole("lht-gen")
	if err != nil {
		t.Fatalf("EnsureRole: %v", err)
	}

	// Verify it exists.
	if !rm.HasRole("lht-gen") {
		t.Fatal("HasRole should be true after EnsureRole")
	}

	err = rm.Release("lht-gen")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}

	if rm.HasRole("lht-gen") {
		t.Error("HasRole should be false after Release")
	}
}

// T4A.1.5 — Release 后 EnsureRole 可重建
func TestRoleManagerReleaseThenReEnsure(t *testing.T) {
	dir := tempRolesDir(t)
	rm := NewRoleManager(dir)

	rc1, err := rm.EnsureRole("lht-gen")
	if err != nil {
		t.Fatalf("first EnsureRole: %v", err)
	}

	if err := rm.Release("lht-gen"); err != nil {
		t.Fatalf("Release: %v", err)
	}

	rc2, err := rm.EnsureRole("lht-gen")
	if err != nil {
		t.Fatalf("second EnsureRole: %v", err)
	}

	if rc2 == nil {
		t.Fatal("re-EnsureRole returned nil")
	}
	// After release and re-create, they should be different instances.
	if rc1 == rc2 {
		t.Error("re-EnsureRole should create a new instance after release")
	}
}

// T4A.1.6 — Release 不存在的 role 报错
func TestRoleManagerReleaseNonexistent(t *testing.T) {
	dir := tempRolesDir(t)
	rm := NewRoleManager(dir)

	err := rm.Release("nonexistent")
	if err == nil {
		t.Error("expected error when releasing nonexistent role")
	}
}

// T4A.1.7 — EnsureRole 不存在的 YAML 文件
func TestRoleManagerEnsureRoleMissingFile(t *testing.T) {
	dir := tempRolesDir(t)
	rm := NewRoleManager(dir)

	_, err := rm.EnsureRole("nonexistent")
	if err == nil {
		t.Error("expected error for missing YAML file")
	}
}

// T4A.1.8 — BuildCommandArgs 构造命令行参数
func TestRoleManagerBuildCommandArgs(t *testing.T) {
	dir := tempRolesDir(t)
	rm := NewRoleManager(dir)

	cfg, err := role.Load(filepath.Join(dir, "lht-gen.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	args := rm.BuildCommandArgs(cfg, "test-token", "http://localhost:8080", "/tmp/reef-home")

	// Verify required flags.
	hasRole := false
	hasSkills := false
	hasServer := false
	hasToken := false
	hasReefHome := false

	for i, a := range args {
		switch a {
		case "--role":
			if i+1 < len(args) && args[i+1] == "lht-gen" {
				hasRole = true
			}
		case "--skills":
			if i+1 < len(args) && args[i+1] == "go,github" {
				hasSkills = true
			}
		case "--server":
			if i+1 < len(args) && args[i+1] == "http://localhost:8080" {
				hasServer = true
			}
		case "--token":
			if i+1 < len(args) && args[i+1] == "test-token" {
				hasToken = true
			}
		}
		if a == "REEF_HOME=/tmp/reef-home" {
			hasReefHome = true
		}
	}

	if !hasRole {
		t.Error("missing --role flag")
	}
	if !hasSkills {
		t.Error("missing --skills flag")
	}
	if !hasServer {
		t.Error("missing --server flag")
	}
	if !hasToken {
		t.Error("missing --token flag")
	}
	if !hasReefHome {
		t.Error("missing REEF_HOME env var")
	}
}

// T4A.1.9 — RoleManager 使用 mock CommandRunner
func TestRoleManagerWithMockRunner(t *testing.T) {
	dir := tempRolesDir(t)
	runner := newMockRunner()
	rm := NewRoleManagerWithRunner(dir, runner)

	rc, err := rm.EnsureRole("lht-gen")
	if err != nil {
		t.Fatalf("EnsureRole: %v", err)
	}

	if rc.Runner != runner {
		t.Error("RoleClient should hold reference to runner")
	}
}

// T4A.1.10 — 三个角色全部加载并验证技能
func TestAllThreeRolesSkills(t *testing.T) {
	dir := tempRolesDir(t)
	rm := NewRoleManager(dir)

	wantSkills := map[string][]string{
		"lht-gen":  {"go", "github"},
		"lht-eval": {"go"},
		"lht-rev":  {"go", "summarize"},
	}

	for name, want := range wantSkills {
		rc, err := rm.EnsureRole(name)
		if err != nil {
			t.Fatalf("EnsureRole(%s): %v", name, err)
		}
		if len(rc.Config.Skills) != len(want) {
			t.Errorf("%s: got %d skills, want %d: %v", name, len(rc.Config.Skills), len(want), rc.Config.Skills)
		}
		for _, w := range want {
			found := false
			for _, s := range rc.Config.Skills {
				if s == w {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: missing skill %q in %v", name, w, rc.Config.Skills)
			}
		}
	}
}
