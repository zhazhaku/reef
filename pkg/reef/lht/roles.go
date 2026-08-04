// Package lht — Role Manager for LHT three-role system.
//
// Implements W4A: loads lht-gen/lht-eval/lht-rev from YAML Role Manifests,
// manages roleClients mapping with lifecycle (EnsureRole/Release),
// and provides exec command construction for launching reef agent processes.
package lht

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zhazhaku/reef/pkg/reef/role"
)

// ────────────────────────────────────────────────────────────
// CommandRunner — exec abstraction for mock-friendly testing.
// ────────────────────────────────────────────────────────────

// CommandRunner abstracts the execution of external commands.
// The real implementation uses os/exec; mock implementations
// are used in tests.
type CommandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// ────────────────────────────────────────────────────────────
// RoleClient — a loaded role with its config and runner.
// ────────────────────────────────────────────────────────────

// RoleClient represents a loaded and ready-to-launch role instance.
type RoleClient struct {
	Name   string
	Config *role.Config
	Runner CommandRunner
}

// ────────────────────────────────────────────────────────────
// RoleManager — manages the mapping of role names to RoleClients.
// ────────────────────────────────────────────────────────────

// RoleManager manages the lifecycle of LHT three-role clients.
// It loads role configs from YAML files, maps role names to RoleClients,
// and supports EnsureRole (idempotent load/register) and Release (cleanup).
type RoleManager struct {
	mu          sync.Mutex
	roleClients map[string]*RoleClient
	rolesDir    string
	runner      CommandRunner
}

// NewRoleManager creates a RoleManager that reads role YAMLs from rolesDir.
// Uses a default real command runner (os/exec).
func NewRoleManager(rolesDir string) *RoleManager {
	return &RoleManager{
		roleClients: make(map[string]*RoleClient),
		rolesDir:    rolesDir,
		runner:      nil, // nil means use real exec.Command
	}
}

// NewRoleManagerWithRunner creates a RoleManager with a custom runner (for testing).
func NewRoleManagerWithRunner(rolesDir string, runner CommandRunner) *RoleManager {
	return &RoleManager{
		roleClients: make(map[string]*RoleClient),
		rolesDir:    rolesDir,
		runner:      runner,
	}
}

// ────────────────────────────────────────────────────────────
// EnsureRole — idempotent role loading and registration.
// ────────────────────────────────────────────────────────────

// EnsureRole loads the named role's YAML config (if not already loaded) and
// returns a RoleClient.  Subsequent calls for the same name return the cached
// instance without re-reading the YAML file.
func (rm *RoleManager) EnsureRole(name string) (*RoleClient, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rc, ok := rm.roleClients[name]; ok {
		return rc, nil
	}

	path := filepath.Join(rm.rolesDir, name+".yaml")
	cfg, err := role.Load(path)
	if err != nil {
		return nil, fmt.Errorf("EnsureRole %s: %w", name, err)
	}

	rc := &RoleClient{
		Name:   name,
		Config: cfg,
		Runner: rm.runner,
	}
	rm.roleClients[name] = rc
	return rc, nil
}

// ────────────────────────────────────────────────────────────
// Release — remove a role from the mapping.
// ────────────────────────────────────────────────────────────

// Release removes the named role from the mapping.
// After release, the next EnsureRole will reload from YAML.
func (rm *RoleManager) Release(name string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, ok := rm.roleClients[name]; !ok {
		return fmt.Errorf("Release: role %q not found", name)
	}

	delete(rm.roleClients, name)
	return nil
}

// ────────────────────────────────────────────────────────────
// HasRole — check if a role is currently loaded.
// ────────────────────────────────────────────────────────────

// HasRole returns true if the named role is currently loaded.
func (rm *RoleManager) HasRole(name string) bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	_, ok := rm.roleClients[name]
	return ok
}

// ────────────────────────────────────────────────────────────
// BuildCommandArgs — construct exec command arguments.
// ────────────────────────────────────────────────────────────

// BuildCommandArgs builds the argument list for executing a role as a
// reef agent process.  The returned slice is suitable for use with
// exec.Command("reef", args...) or equivalent.
//
// Parameters:
//   - cfg: the role configuration (name + skills)
//   - token: auth token for the reef server
//   - server: reef server address (e.g. "http://localhost:8080")
//   - reefHome: REEF_HOME directory
func (rm *RoleManager) BuildCommandArgs(cfg *role.Config, token, server, reefHome string) []string {
	args := []string{
		"REEF_HOME=" + reefHome,
		"agent",
		"--role", cfg.Name,
	}

	if len(cfg.Skills) > 0 {
		args = append(args, "--skills", strings.Join(cfg.Skills, ","))
	}

	if server != "" {
		args = append(args, "--server", server)
	}
	if token != "" {
		args = append(args, "--token", token)
	}

	return args
}

// ────────────────────────────────────────────────────────────
// Launch — execute the role client process.
// ────────────────────────────────────────────────────────────

// Launch starts the role client process using the configured runner.
// If the runner is nil, this is a no-op (real exec not wired yet; see W4B).
func (rc *RoleClient) Launch(token, server, reefHome string) error {
	if rc.Runner == nil {
		// Real exec not yet wired; W4B will integrate with the actual reef
		// agent launch path.
		return nil
	}

	args := []string{
		"REEF_HOME=" + reefHome,
		"agent",
		"--role", rc.Name,
	}
	if len(rc.Config.Skills) > 0 {
		args = append(args, "--skills", strings.Join(rc.Config.Skills, ","))
	}
	if server != "" {
		args = append(args, "--server", server)
	}
	if token != "" {
		args = append(args, "--token", token)
	}

	_, err := rc.Runner.Run("reef", args...)
	return err
}
