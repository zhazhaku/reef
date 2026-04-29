// Package reef implements the `picoclaw reef` subcommands for interacting
// with a running Reef Server (status, task management, etc.).
package reef

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/pkg/reef/server"
)

func NewReefCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reef",
		Short: "Interact with a Reef Server",
		Long:  `Query status, manage tasks, and interact with a running Reef Server.`,
	}

	cmd.AddCommand(
		newReefServerCommand(),
		newStatusCommand(),
		newTasksCommand(),
		newSubmitCommand(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// reef reef-server (standalone server)
// ---------------------------------------------------------------------------

func newReefServerCommand() *cobra.Command {
	var (
		wsAddr    string
		adminAddr string
		token     string
		maxQueue  int
		maxEscal  int
	)

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start a standalone Reef Server",
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

// ---------------------------------------------------------------------------
// reef status
// ---------------------------------------------------------------------------

func newStatusCommand() *cobra.Command {
	var serverURL string
	var token string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Reef Server status",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			url := serverURL + "/admin/status"
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("connect to server: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
			}

			var status map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}

			printStatus(status)
			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "http://localhost:8081", "Reef Server admin URL")
	cmd.Flags().StringVar(&token, "token", "", "Authentication token")

	return cmd
}

func printStatus(status map[string]any) {
	fmt.Println()
	fmt.Println("Reef Server Status")
	fmt.Println("═══════════════════════════════════════════════")

	if v, ok := status["server_version"].(string); ok {
		fmt.Printf("  Version:   %s\n", v)
	}
	if v, ok := status["uptime_ms"].(float64); ok {
		fmt.Printf("  Uptime:    %s\n", formatDuration(v))
	}

	// Connected clients
	if clients, ok := status["connected_clients"].([]any); ok {
		fmt.Printf("\n  Connected Clients: %d\n", len(clients))
		if len(clients) > 0 {
			w := tabwriter.NewWriter(os.Stdout, 4, 2, 2, ' ', 0)
			fmt.Fprintln(w, "    ID\tRole\tSkills\tLoad")
			fmt.Fprintln(w, "    ──\t────\t──────\t────")
			for _, c := range clients {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				id := fmt.Sprintf("%v", cm["client_id"])
				role := fmt.Sprintf("%v", cm["role"])
				skills := formatStringSlice(cm["skills"])
				load := fmt.Sprintf("%v/%v", cm["current_load"], cm["capacity"])
				fmt.Fprintf(w, "    %s\t%s\t%s\t%s\n", id, role, skills, load)
			}
			w.Flush()
		}
	}

	// Disconnected / Stale
	if v, ok := status["disconnected_count"].(float64); ok && v > 0 {
		fmt.Printf("  Disconnected: %d\n", int(v))
	}
	if v, ok := status["stale_count"].(float64); ok && v > 0 {
		fmt.Printf("  Stale:        %d\n", int(v))
	}

	fmt.Println()
}

// ---------------------------------------------------------------------------
// reef tasks
// ---------------------------------------------------------------------------

func newTasksCommand() *cobra.Command {
	var serverURL string
	var token string
	var roleFilter string
	var statusFilter string

	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "List tasks on the Reef Server",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			url := serverURL + "/admin/tasks"
			if roleFilter != "" || statusFilter != "" {
				url += "?"
				if roleFilter != "" {
					url += "role=" + roleFilter + "&"
				}
				if statusFilter != "" {
					url += "status=" + statusFilter + "&"
				}
			}

			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("connect to server: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
			}

			var tasks map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}

			printTasks(tasks)
			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "http://localhost:8081", "Reef Server admin URL")
	cmd.Flags().StringVar(&token, "token", "", "Authentication token")
	cmd.Flags().StringVarP(&roleFilter, "role", "r", "", "Filter by role")
	cmd.Flags().StringVarP(&statusFilter, "status", "s", "", "Filter by status")

	return cmd
}

func printTasks(data map[string]any) {
	fmt.Println()
	fmt.Println("Reef Tasks")
	fmt.Println("═══════════════════════════════════════════════")

	if stats, ok := data["stats"].(map[string]any); ok {
		fmt.Printf("  Total: %v  Success: %v  Failed: %v  Queued: %v  Running: %v\n",
			stats["total"], stats["success"], stats["failed"], stats["queued"], stats["running"])
	}

	for _, section := range []struct {
		key  string
		name string
	}{
		{"queued_tasks", "Queued"},
		{"inflight_tasks", "In-Flight"},
		{"completed_tasks", "Completed"},
	} {
		tasks, ok := data[section.key].([]any)
		if !ok || len(tasks) == 0 {
			continue
		}
		fmt.Printf("\n  %s:\n", section.name)
		w := tabwriter.NewWriter(os.Stdout, 4, 2, 2, ' ', 0)
		fmt.Fprintln(w, "    ID\tStatus\tRole\tAssigned To\t")
		fmt.Fprintln(w, "    ──\t──────\t────\t───────────\t")
		for _, t := range tasks {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			id := fmt.Sprintf("%v", tm["task_id"])
			status := fmt.Sprintf("%v", tm["status"])
			role := fmt.Sprintf("%v", tm["required_role"])
			assigned := fmt.Sprintf("%v", tm["assigned_client_id"])
			if assigned == "" || assigned == "<nil>" {
				assigned = "-"
			}
			fmt.Fprintf(w, "    %s\t%s\t%s\t%s\n", id, status, role, assigned)
		}
		w.Flush()
	}

	fmt.Println()
}

// ---------------------------------------------------------------------------
// reef submit
// ---------------------------------------------------------------------------

func newSubmitCommand() *cobra.Command {
	var serverURL string
	var token string
	var role string
	var skills []string

	cmd := &cobra.Command{
		Use:   "submit <instruction>",
		Short: "Submit a task to the Reef Server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instruction := args[0]

			body, _ := json.Marshal(map[string]any{
				"instruction":     instruction,
				"required_role":   role,
				"required_skills": skills,
				"max_retries":     2,
			})

			url := serverURL + "/admin/tasks"
			req, err := http.NewRequest(http.MethodPost, url, jsonBodyReader(body))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("submit task: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusAccepted {
				respBody, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
			}

			var result map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}

			fmt.Printf("✓ Task submitted: %s\n", result["task_id"])
			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "http://localhost:8081", "Reef Server admin URL")
	cmd.Flags().StringVar(&token, "token", "", "Authentication token")
	cmd.Flags().StringVarP(&role, "role", "r", "", "Required role for the task")
	cmd.Flags().StringArrayVar(&skills, "skills", nil, "Required skills")

	return cmd
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func formatDuration(ms float64) string {
	seconds := int(ms / 1000)
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	seconds = seconds % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	hours := minutes / 60
	minutes = minutes % 60
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

func formatStringSlice(v any) string {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return "-"
	}
	var parts []string
	for _, item := range arr {
		parts = append(parts, fmt.Sprintf("%v", item))
	}
	if len(parts) > 3 {
		return fmt.Sprintf("%s (+%d more)", joinStrings(parts[:3]), len(parts)-3)
	}
	return joinStrings(parts)
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

func jsonBodyReader(data []byte) *jsonBody {
	return &jsonBody{data: data, pos: 0}
}

type jsonBody struct {
	data []byte
	pos  int
}

func (b *jsonBody) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}
