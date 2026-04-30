// PicoClaw - Ultra-lightweight personal AI agent
//
// `reef server` command — starts Reef in Server mode:
//   - Gateway (channels + LLM + AgentLoop)
//   - Reef Server (WebSocket + Admin + UI)
//   - Hermes Coordinator (restricted tool set)

package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/zhazhaku/reef/cmd/reef/internal"
	"github.com/zhazhaku/reef/pkg/config"
	"github.com/zhazhaku/reef/pkg/gateway"
	"github.com/zhazhaku/reef/pkg/logger"
	"github.com/zhazhaku/reef/pkg/utils"
)

func NewServerCommand() *cobra.Command {
	var debug bool
	var noTruncate bool
	var allowEmpty bool
	var wsAddr string
	var adminAddr string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start PicoClaw in server mode (Reef + Hermes Coordinator)",
		Long: `Start PicoClaw in server mode.

This command launches PicoClaw as a team coordinator that:
  • Starts the Reef Server for managing connected clients
  • Enables Hermes Coordinator mode (restricted tool set)
  • Delegates complex tasks to connected client agents
  • Handles simple greetings and meta-questions directly

The server acts as the brain of a multi-agent team, routing
tasks to specialized clients and aggregating their results.`,
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if noTruncate && !debug {
				return fmt.Errorf("the --no-truncate option can only be used in conjunction with --debug (-d)")
			}
			if noTruncate {
				utils.SetDisableTruncation(true)
				logger.Info("String truncation is globally disabled via 'no-truncate' flag")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Load original config
			configPath := internal.GetConfigPath()
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("error loading config: %w", err)
			}

			// Force Hermes Coordinator mode
			cfg.Hermes.Mode = "coordinator"

			// Auto-configure Reef Server if not already configured
			ensureReefServerConfig(cfg, wsAddr, adminAddr)

			// Write patched config to a temporary file
			tmpConfig, err := writeTempConfig(cfg)
			if err != nil {
				return fmt.Errorf("error preparing server config: %w", err)
			}
			defer os.Remove(tmpConfig)

			// Print server mode banner
			printServerBanner()

			// Launch gateway with patched config
			return gateway.Run(debug, internal.GetPicoclawHome(), tmpConfig, allowEmpty)
		},
	}

	cmd.Flags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging")
	cmd.Flags().BoolVarP(&noTruncate, "no-truncate", "T", false, "Disable string truncation in debug logs")
	cmd.Flags().BoolVarP(&allowEmpty, "allow-empty", "E", false, "Continue starting even when no default model is configured")
	cmd.Flags().StringVar(&wsAddr, "ws-addr", "", "WebSocket listen address for Reef Server (default: :9999)")
	cmd.Flags().StringVar(&adminAddr, "admin-addr", "", "Admin HTTP listen address for Reef Server (default: :8081)")

	return cmd
}

// ensureReefServerConfig ensures the swarm channel is configured with mode=server.
// If it's already configured in server mode, it preserves existing settings.
func ensureReefServerConfig(cfg *config.Config, wsAddr, adminAddr string) {
	if wsAddr == "" {
		wsAddr = ":9999"
	}
	if adminAddr == "" {
		adminAddr = ":8081"
	}

	ch, exists := cfg.Channels["swarm"]
	if exists {
		// Already configured — check if mode is server
		decoded, _ := ch.GetDecoded()
		if decoded != nil {
			if settings, ok := decoded.(*config.SwarmSettings); ok && settings.Mode == "server" {
				// Already in server mode, apply overrides if provided
				if cmdFlagChanged(wsAddr, ":9999") {
					settings.WSAddr = wsAddr
				}
				if cmdFlagChanged(adminAddr, ":8081") {
					settings.AdminAddr = adminAddr
				}
				return
			}
		}
	}

	// Not configured or not in server mode — build a new Channel
	// We build the raw JSON config and re-load it through the standard
	// config loading path to ensure proper initialization.
	settingsJSON, _ := json.Marshal(map[string]any{
		"enabled":    true,
		"mode":       "server",
		"ws_addr":    wsAddr,
		"admin_addr": adminAddr,
	})

	ch = &config.Channel{}
	ch.SetName("swarm")
	ch.Enabled = true
	ch.Type = "swarm"
	_ = ch.Settings.UnmarshalJSON(settingsJSON)
	cfg.Channels["swarm"] = ch
}

// writeTempConfig writes the patched config to a temporary file.
// This avoids modifying the user's actual config.json.
func writeTempConfig(cfg *config.Config) (string, error) {
	home := internal.GetPicoclawHome()
	tmpDir := filepath.Join(home, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", err
	}

	tmpFile, err := os.CreateTemp(tmpDir, "reef-server-*.json")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("error marshaling config: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

// cmdFlagChanged returns true if the value differs from the default.
func cmdFlagChanged(value, defaultValue string) bool {
	return value != defaultValue
}

// printServerBanner prints the server mode startup banner.
func printServerBanner() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                   PicoClaw Server Mode                      ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Hermes Mode:  Coordinator                                  ║")
	fmt.Println("║  Role:         Team Coordinator (task delegation)           ║")
	fmt.Println("║  Tool Policy:  Coordination tools only                      ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
}
