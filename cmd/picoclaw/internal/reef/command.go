package reef

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/pkg/reef/server"
)

func NewReefCommand() *cobra.Command {
	var (
		wsAddr     string
		adminAddr  string
		token      string
		maxQueue   int
		maxEscal   int
	)

	cmd := &cobra.Command{
		Use:   "reef-server",
		Short: "Start Reef distributed swarm server",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := server.DefaultConfig()
			cfg.WebSocketAddr = wsAddr
			cfg.AdminAddr = adminAddr
			cfg.Token = token
			cfg.QueueMaxLen = maxQueue
			cfg.MaxEscalations = maxEscal

			srv := server.NewServer(cfg, nil)
			if err := srv.Start(); err != nil {
				return fmt.Errorf("start reef server: %w", err)
			}

			fmt.Printf("✓ Reef Server started\n")
			fmt.Printf("  WebSocket: %s\n", wsAddr)
			fmt.Printf("  Admin:     %s\n", adminAddr)
			fmt.Println("Press Ctrl+C to stop")

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
			<-sigChan

			fmt.Println("\nShutting down...")
			return srv.Stop()
		},
	}

	cmd.Flags().StringVar(&wsAddr, "ws-addr", ":8080", "WebSocket listen address")
	cmd.Flags().StringVar(&adminAddr, "admin-addr", ":8081", "Admin HTTP listen address")
	cmd.Flags().StringVar(&token, "token", "", "Shared authentication token (x-reef-token)")
	cmd.Flags().IntVar(&maxQueue, "max-queue", 1000, "Maximum queued tasks")
	cmd.Flags().IntVar(&maxEscal, "max-escalations", 2, "Max escalation attempts per task")

	return cmd
}
