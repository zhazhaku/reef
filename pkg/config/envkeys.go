// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package config

import (
	"os"
	"path/filepath"

	"github.com/sipeed/picoclaw/pkg"
)

// Runtime environment variable keys for the picoclaw process.
// These control the location of files and binaries at runtime and are read
// directly via os.Getenv / os.LookupEnv. All picoclaw-specific keys use the
// PICOCLAW_ prefix. Reference these constants instead of inline string
// literals to keep all supported knobs visible in one place and to prevent
// typos.
const (
	// EnvHome overrides the base directory for all picoclaw data
	// (config, workspace, skills, auth store, …).
	// Default: ~/.picoclaw
	EnvHome = "PICOCLAW_HOME"

	// EnvConfig overrides the full path to the JSON config file.
	// Default: $PICOCLAW_HOME/config.json
	EnvConfig = "PICOCLAW_CONFIG"

	// EnvBuiltinSkills overrides the directory from which built-in
	// skills are loaded.
	// Default: <cwd>/skills
	EnvBuiltinSkills = "PICOCLAW_BUILTIN_SKILLS"

	// EnvBinary overrides the path to the picoclaw executable.
	// Used by the web launcher when spawning the gateway subprocess.
	// Default: resolved from the same directory as the current executable.
	EnvBinary = "PICOCLAW_BINARY"

	// EnvGatewayHost overrides the host address for the gateway server.
	// Default: "localhost"
	EnvGatewayHost = "PICOCLAW_GATEWAY_HOST"
)

func GetHome() string {
	// 1. Explicit env var override (highest priority)
	if picoclawHome := os.Getenv(EnvHome); picoclawHome != "" {
		return picoclawHome
	}

	// 2. Check for existing .picoclaw next to the executable (portable mode).
	//    This enables self-contained deployments on embedded devices, Android,
	//    or any scenario where all data should live alongside the binary.
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		portableHome := filepath.Join(exeDir, pkg.DefaultPicoClawHome)
		if info, err := os.Stat(portableHome); err == nil && info.IsDir() {
			return portableHome
		}
	}

	// 3. Check for existing .picoclaw in the current working directory.
	//    This supports running the program from a project/workspace directory
	//    that contains its own .picoclaw configuration.
	if wd, err := os.Getwd(); err == nil {
		cwdHome := filepath.Join(wd, pkg.DefaultPicoClawHome)
		if info, err := os.Stat(cwdHome); err == nil && info.IsDir() {
			return cwdHome
		}
	}

	// 4. Default: ~/.picoclaw
	homePath, _ := os.UserHomeDir()
	if homePath != "" {
		return filepath.Join(homePath, pkg.DefaultPicoClawHome)
	}

	return "."
}

// GetOrCreateHome returns the picoclaw home directory, creating it if necessary.
// Priority: $PICOCLAW_HOME > .picoclaw next to exe (create if writable) > .picoclaw in cwd (create if writable) > ~/.picoclaw
// This is used by onboard to ensure fresh installs default to the exe directory
// when it is writable (embedded devices, Android, portable deployments).
func GetOrCreateHome() string {
	// 1. Explicit env var override
	if picoclawHome := os.Getenv(EnvHome); picoclawHome != "" {
		return picoclawHome
	}

	// 2. Check for existing .picoclaw next to the executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		portableHome := filepath.Join(exeDir, pkg.DefaultPicoClawHome)
		if info, err := os.Stat(portableHome); err == nil && info.IsDir() {
			return portableHome
		}
	}

	// 3. Check for existing .picoclaw in the current working directory
	if wd, err := os.Getwd(); err == nil {
		cwdHome := filepath.Join(wd, pkg.DefaultPicoClawHome)
		if info, err := os.Stat(cwdHome); err == nil && info.IsDir() {
			return cwdHome
		}
		// Not yet created — try to create it in cwd (first-run onboard)
		if err := os.MkdirAll(cwdHome, 0o755); err == nil {
			return cwdHome
		}
	}

	// 4. Default: ~/.picoclaw
	homePath, _ := os.UserHomeDir()
	if homePath != "" {
		return filepath.Join(homePath, pkg.DefaultPicoClawHome)
	}

	return "."
}
