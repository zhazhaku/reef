// Reef - Distributed multi-agent swarm orchestration system
// Based on Reef (github.com/sipeed/reef)
// License: MIT
//
// Copyright (c) 2026 Reef contributors

package config

import (
	"os"
	"path/filepath"

	"github.com/zhazhaku/reef/pkg"
)

// Runtime environment variable keys for the reef process.
// These control the location of files and binaries at runtime and are read
// directly via os.Getenv / os.LookupEnv. All reef-specific keys use the
// REEF_ prefix. Reference these constants instead of inline string
// literals to keep all supported knobs visible in one place and to prevent
// typos.
const (
	// EnvHome overrides the base directory for all reef data
	// (config, workspace, skills, auth store, …).
	// Default: ~/.reef
	EnvHome = "REEF_HOME"

	// EnvHomeLegacy is the old REEF_HOME env var for backward compatibility.
	EnvHomeLegacy = "REEF_HOME"

	// EnvConfig overrides the full path to the JSON config file.
	// Default: $REEF_HOME/config.json
	EnvConfig = "REEF_CONFIG"

	// EnvBuiltinSkills overrides the directory from which built-in
	// skills are loaded.
	// Default: <cwd>/skills
	EnvBuiltinSkills = "REEF_BUILTIN_SKILLS"

	// EnvBinary overrides the path to the reef executable.
	// Used by the web launcher when spawning the gateway subprocess.
	// Default: resolved from the same directory as the current executable.
	EnvBinary = "REEF_BINARY"

	// EnvGatewayHost overrides the host address for the gateway server.
	// Default: "localhost"
	EnvGatewayHost = "REEF_GATEWAY_HOST"
)

func GetHome() string {
	// 1. Explicit env var override: REEF_HOME (highest priority)
	if reefHome := os.Getenv(EnvHome); reefHome != "" {
		return reefHome
	}

	// 1b. Backward compat: REEF_HOME fallback
	if reefHome := os.Getenv(EnvHomeLegacy); reefHome != "" {
		return reefHome
	}

	// 2. Check for existing .reef next to the executable (portable mode).
	//    This enables self-contained deployments on embedded devices, Android,
	//    or any scenario where all data should live alongside the binary.
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		portableHome := filepath.Join(exeDir, pkg.DefaultReefHome)
		if info, err := os.Stat(portableHome); err == nil && info.IsDir() {
			return portableHome
		}
		// Fallback: check old .reef directory
		portableHomeOld := filepath.Join(exeDir, pkg.DefaultReefHome)
		if info, err := os.Stat(portableHomeOld); err == nil && info.IsDir() {
			return portableHomeOld
		}
	}

	// 3. Check for existing .reef in the current working directory.
	//    This supports running the program from a project/workspace directory
	//    that contains its own .reef configuration.
	if wd, err := os.Getwd(); err == nil {
		cwdHome := filepath.Join(wd, pkg.DefaultReefHome)
		if info, err := os.Stat(cwdHome); err == nil && info.IsDir() {
			return cwdHome
		}
		// Fallback: check old .reef directory
		cwdHomeOld := filepath.Join(wd, pkg.DefaultReefHome)
		if info, err := os.Stat(cwdHomeOld); err == nil && info.IsDir() {
			return cwdHomeOld
		}
	}

	// 4. Default: ~/.reef
	homePath, _ := os.UserHomeDir()
	if homePath != "" {
		return filepath.Join(homePath, pkg.DefaultReefHome)
	}

	return "."
}

// GetOrCreateHome returns the reef home directory, creating it if necessary.
// Priority: $REEF_HOME > .reef next to exe (create if writable) > .reef in cwd (create if writable) > ~/.reef
// This is used by onboard to ensure fresh installs default to the exe directory
// when it is writable (embedded devices, Android, portable deployments).
func GetOrCreateHome() string {
	// 1. Explicit env var override: REEF_HOME
	if reefHome := os.Getenv(EnvHome); reefHome != "" {
		return reefHome
	}

	// 1b. Backward compat: REEF_HOME fallback
	if reefHome := os.Getenv(EnvHomeLegacy); reefHome != "" {
		return reefHome
	}

	// 2. Check for existing .reef next to the executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		portableHome := filepath.Join(exeDir, pkg.DefaultReefHome)
		if info, err := os.Stat(portableHome); err == nil && info.IsDir() {
			return portableHome
		}
		// Fallback: old .reef directory
		portableHomeOld := filepath.Join(exeDir, pkg.DefaultReefHome)
		if info, err := os.Stat(portableHomeOld); err == nil && info.IsDir() {
			return portableHomeOld
		}
	}

	// 3. Check for existing .reef in the current working directory
	if wd, err := os.Getwd(); err == nil {
		cwdHome := filepath.Join(wd, pkg.DefaultReefHome)
		if info, err := os.Stat(cwdHome); err == nil && info.IsDir() {
			return cwdHome
		}
		// Fallback: old .reef directory
		cwdHomeOld := filepath.Join(wd, pkg.DefaultReefHome)
		if info, err := os.Stat(cwdHomeOld); err == nil && info.IsDir() {
			return cwdHomeOld
		}
		// Not yet created — try to create it in cwd (first-run onboard)
		if err := os.MkdirAll(cwdHome, 0o755); err == nil {
			return cwdHome
		}
	}

	// 4. Default: ~/.reef
	homePath, _ := os.UserHomeDir()
	if homePath != "" {
		return filepath.Join(homePath, pkg.DefaultReefHome)
	}

	return "."
}
