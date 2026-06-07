// Package client implements the `reef client` command — a standalone
// Reef worker node that connects to a Reef Server, receives tasks, and
// executes them using Reef's AgentLoop.
package client

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/zhazhaku/reef/cmd/reef/internal"
	"github.com/zhazhaku/reef/pkg/agent"
	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/config"
	"github.com/zhazhaku/reef/pkg/logger"
	"github.com/zhazhaku/reef/pkg/providers"
	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/client"
	"github.com/zhazhaku/reef/pkg/utils"
)

func NewClientCommand() *cobra.Command {
	var (
		serverURL  string
		clientID   string
		role       string
		skills     []string
		capacity   int
		token      string
		debug      bool
		noTruncate bool
		model      string
	)

	cmd := &cobra.Command{
		Use:   "client",
		Short: "Start Reef as a Reef worker node",
		Long: `Start Reef as a standalone worker node that connects to a Reef Server,
receives tasks, and executes them using Reef's AgentLoop.

This is the counterpart to 'reef server'. While the server coordinates
and delegates, the client executes tasks using all available tools.

Examples:
  reef client --server ws://reef-server:9999 --role executor --skills web_search,code_execution
  reef client --server wss://reef.example.com:9999 --token my-secret --role coder`,
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if noTruncate && !debug {
				return fmt.Errorf("--no-truncate requires --debug")
			}
			if serverURL == "" {
				return fmt.Errorf("--server is required")
			}
			if role == "" {
				return fmt.Errorf("--role is required")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			if noTruncate {
				utils.SetDisableTruncation(true)
			}

			// Load config for model settings
			configPath := internal.GetConfigPath()
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Force Hermes Executor mode
			cfg.Hermes.Mode = "executor"

			// CLI --model overrides config default
			if model != "" {
				cfg.Agents.Defaults.ModelName = model
			}

			// Build connector options
			connectorOpts := client.ConnectorOptions{
				ServerURL: serverURL,
				Token:     token,
				ClientID:  clientID,
				Role:      role,
				Skills:    skills,
				Capacity:  capacity,
			}

			return runClient(cfg, connectorOpts, debug)
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "", "Reef Server WebSocket URL (required)")
	cmd.Flags().StringVar(&clientID, "id", "", "Client ID (auto-generated if empty)")
	cmd.Flags().StringVar(&role, "role", "", "Client role (required)")
	cmd.Flags().StringSliceVar(&skills, "skills", nil, "Skills this client supports (comma-separated or repeated)")
	cmd.Flags().IntVarP(&capacity, "capacity", "c", 3, "Max concurrent tasks")
	cmd.Flags().StringVar(&token, "token", "", "Authentication token")
	cmd.Flags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging")
	cmd.Flags().BoolVarP(&noTruncate, "no-truncate", "T", false, "Disable string truncation in debug logs")
	cmd.Flags().StringVar(&model, "model", "", "Override default LLM model (must exist in model_list)")

	return cmd
}

// runClient starts the Reef client with full AgentLoop integration.
func runClient(cfg *config.Config, connectorOpts client.ConnectorOptions, debug bool) error {
	if debug {
		logger.SetLevelFromString("debug")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT/SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	printClientBanner(connectorOpts, cfg.Agents.Defaults.ModelName)

	// Create LLM provider
	provider, _, err := providers.CreateProvider(cfg)
	if err != nil {
		return fmt.Errorf("create LLM provider: %w", err)
	}

	// Create message bus and AgentLoop
	msgBus := bus.NewMessageBus()
	al := agent.NewAgentLoop(cfg, msgBus, provider)

	// Set Hermes Executor mode
	al.SetHermesMode(agent.HermesExecutor)

	// Create connector
	connector := client.NewConnector(connectorOpts)

	// Create task runner
	runner := client.NewTaskRunner(client.TaskRunnerOptions{
		Connector: connector,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			// Execute the task using the AgentLoop
			return al.ProcessDirectWithChannel(ctx, instruction, "reef-task", "swarm", "reef-task")
		},
	})

	// Connect to server
	if err := connector.Connect(ctx); err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}

	fmt.Printf("✓ Connected to %s\n", connectorOpts.ServerURL)
	fmt.Printf("  Client ID: %s\n", connectorOpts.ClientID)
	fmt.Printf("  Role:      %s\n", connectorOpts.Role)
	fmt.Printf("  Capacity:  %d\n", connectorOpts.Capacity)
	fmt.Println("Press Ctrl+C to stop")

	// Process incoming messages
	go processMessages(ctx, cancel, connector, runner)

	// Block until context is done
	<-ctx.Done()

	_ = connector.Close()
	return nil
}

// processMessages handles incoming messages from the Reef Server.
func processMessages(ctx context.Context, cancel context.CancelFunc, connector *client.Connector, runner *client.TaskRunner) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-connector.Messages():
			if !ok {
				return
			}
			switch msg.MsgType {
			case "task_dispatch":
				var payload struct {
					TaskID      string `json:"task_id"`
					Instruction string `json:"instruction"`
					MaxRetries  int    `json:"max_retries"`
				}
				if err := msg.DecodePayload(&payload); err != nil {
					continue
				}
				runner.StartTask(payload.TaskID, payload.Instruction, payload.MaxRetries)

			case "cancel":
				var payload struct {
					TaskID string `json:"task_id"`
				}
				if err := msg.DecodePayload(&payload); err != nil {
					continue
				}
				runner.CancelTask(payload.TaskID)

			case "pause":
				var payload struct {
					TaskID string `json:"task_id"`
				}
				if err := msg.DecodePayload(&payload); err != nil {
					continue
				}
				runner.PauseTask(payload.TaskID)

			case "resume":
				var payload struct {
					TaskID string `json:"task_id"`
				}
				if err := msg.DecodePayload(&payload); err != nil {
					continue
				}
				runner.ResumeTask(payload.TaskID)

			case "shutdown":
				var payload reef.ShutdownPayload
				_ = msg.DecodePayload(&payload)
				reason := payload.Reason
				if reason == "" {
					reason = "requested by server"
				}
				fmt.Printf("\n🛑 Shutdown received: %s\n", reason)
				fmt.Println("   Waiting for running tasks to finish...")
				cancel() // triggers context cancellation → graceful shutdown
				return
			}
		}
	}
}

// printClientBanner prints the client mode startup banner.
func printClientBanner(opts client.ConnectorOptions, modelName string) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  Reef Client Mode                       ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Hermes Mode:  Executor                                     ║")
	fmt.Printf("║  Role:         %-46s ║\n", opts.Role)
	if modelName != "" {
		fmt.Printf("║  Model:        %-46s ║\n", modelName)
	}
	if len(opts.Skills) > 0 {
		fmt.Printf("║  Skills:       %-46s ║\n", strings.Join(opts.Skills, ", "))
	}
	fmt.Println("║  Tool Policy:  All tools available                          ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
}
