// Package lht — REST API + SSE handler for LHT Engine (W5A).
//
// Provides /api/v2/lht/* REST endpoints and SSE event forwarding
// via the engine's EventHook mechanism.
package lht

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ────────────────────────────────────────────────────────────
// SSEEvent — event structure for SSE subscribers.
// ────────────────────────────────────────────────────────────

// SSEEvent is a structured event pushed to SSE subscribers.
type SSEEvent struct {
	Type   string `json:"type"`   // "lht_state", "lht_log", "lht_milestone"
	GoalID string `json:"goal_id"`
	Data   any    `json:"data"`
}

// ────────────────────────────────────────────────────────────
// APIHandler — HTTP handler for LHT REST API.
// ────────────────────────────────────────────────────────────

// APIHandler serves the LHT REST API and manages SSE subscriptions.
type APIHandler struct {
	engine     *Engine
	store      *Store
	mu         sync.Mutex
	sseClients map[chan SSEEvent]struct{}
}

// NewAPIHandler creates a new API handler with the given engine and store.
func NewAPIHandler(engine *Engine, store *Store) *APIHandler {
	h := &APIHandler{
		engine:     engine,
		store:      store,
		sseClients: make(map[chan SSEEvent]struct{}),
	}

	// Wire the engine's EventHook to forward to SSE subscribers.
	engine.EventHook = func(eventType, goalID string, data any) {
		h.PublishSSE(SSEEvent{Type: eventType, GoalID: goalID, Data: data})
	}

	return h
}

// ────────────────────────────────────────────────────────────
// SSE subscription management
// ────────────────────────────────────────────────────────────

// SubscribeSSE registers a channel to receive SSE events.
func (h *APIHandler) SubscribeSSE(ch chan SSEEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sseClients[ch] = struct{}{}
}

// UnsubscribeSSE removes a channel from SSE event delivery.
func (h *APIHandler) UnsubscribeSSE(ch chan SSEEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sseClients, ch)
}

// PublishSSE sends an event to all subscribed SSE clients.
func (h *APIHandler) PublishSSE(ev SSEEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.sseClients {
		select {
		case ch <- ev:
		default:
			// Drop if client is slow.
		}
	}
}

// SSEClientCount returns the number of active SSE subscribers.
func (h *APIHandler) SSEClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sseClients)
}

// ────────────────────────────────────────────────────────────
// RegisterRoutes — register LHT routes on the given mux.
// ────────────────────────────────────────────────────────────

// RegisterRoutes registers all /api/v2/lht/* routes on the given ServeMux.
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v2/lht/list", h.handleList)
	mux.HandleFunc("/api/v2/lht/", h.handleLHT)
}

// ────────────────────────────────────────────────────────────
// handleLHT — dispatches /api/v2/lht/{id}[/{action}]
// ────────────────────────────────────────────────────────────

func (h *APIHandler) handleLHT(w http.ResponseWriter, r *http.Request) {
	// Strip "/api/v2/lht/" prefix.
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/lht/")
	path = strings.TrimSuffix(path, "/")

	if path == "" {
		// GET /api/v2/lht/ → root info
		h.handleRoot(w, r)
		return
	}

	parts := strings.SplitN(path, "/", 2)
	goalID := parts[0]

	if goalID == "" || !isValidGoalID(goalID) {
		http.Error(w, `{"error":"invalid goal_id"}`, http.StatusBadRequest)
		return
	}

	if len(parts) == 1 {
		// GET /api/v2/lht/{id}
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		h.handleDetail(w, r, goalID)
		return
	}

	// /api/v2/lht/{id}/{action}
	action := parts[1]
	h.handleAction(w, r, goalID, action)
}

// ────────────────────────────────────────────────────────────
// handleList — GET /api/v2/lht/list
// ────────────────────────────────────────────────────────────

func (h *APIHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	goals := h.listGoals()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": goals,
		"total": len(goals),
	})
}

// listGoals walks the store's BasePath to discover all goals.
func (h *APIHandler) listGoals() []map[string]interface{} {
	entries, err := os.ReadDir(h.store.BasePath)
	if err != nil {
		return nil
	}

	var results []map[string]interface{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		goalID := entry.Name()
		if !isValidGoalID(goalID) {
			continue
		}

		goal, err := h.store.LoadGoal(goalID)
		if err != nil || goal == nil {
			continue
		}

		results = append(results, map[string]interface{}{
			"goal_id":     goal.GoalID,
			"description": goal.Description,
			"state":       string(goal.State),
			"created_at":  goal.CreatedAt,
			"updated_at":  goal.UpdatedAt,
		})
	}

	// Sort by updated_at descending.
	sort.Slice(results, func(i, j int) bool {
		ti, _ := results[i]["updated_at"].(interface{})
		tj, _ := results[j]["updated_at"].(interface{})
		// Fallback: sort by goal_id.
		gi, _ := results[i]["goal_id"].(string)
		gj, _ := results[j]["goal_id"].(string)
		// Try time comparison.
		type timeI interface{ String() string }
		if tiS, ok := ti.(timeI); ok {
			if tjS, ok := tj.(timeI); ok {
				return tiS.String() > tjS.String()
			}
		}
		return gi < gj
	})

	if results == nil {
		results = []map[string]interface{}{}
	}
	return results
}

// ────────────────────────────────────────────────────────────
// handleDetail — GET /api/v2/lht/{id}
// ────────────────────────────────────────────────────────────

func (h *APIHandler) handleDetail(w http.ResponseWriter, r *http.Request, goalID string) {
	goal, err := h.store.LoadGoal(goalID)
	if err != nil || goal == nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	plan, _ := h.store.LoadPlan(goalID)
	budget, _ := h.store.LoadBudget(goalID)
	reviews, _ := h.store.LoadReviewRecords(goalID)

	response := map[string]interface{}{
		"goal": map[string]interface{}{
			"goal_id":     goal.GoalID,
			"description": goal.Description,
			"state":       string(goal.State),
			"created_at":  goal.CreatedAt,
			"updated_at":  goal.UpdatedAt,
			"budget_ref":  goal.BudgetRef,
			"plan_ref":    goal.PlanRef,
		},
	}

	if plan != nil {
		response["plan"] = plan
	}
	if budget != nil {
		response["budget"] = budget
	}
	if reviews != nil {
		response["reviews"] = reviews
	} else {
		response["reviews"] = []interface{}{}
	}

	writeJSON(w, http.StatusOK, response)
}

// ────────────────────────────────────────────────────────────
// handleRoot — GET /api/v2/lht/
// ────────────────────────────────────────────────────────────

func (h *APIHandler) handleRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"service": "lht-engine",
		"version": "1.0",
		"endpoints": []string{
			"GET  /api/v2/lht/list",
			"GET  /api/v2/lht/{id}",
			"GET  /api/v2/lht/{id}/logs?page=1&size=50",
			"POST /api/v2/lht/{id}/pause",
			"POST /api/v2/lht/{id}/resume",
			"POST /api/v2/lht/{id}/stop",
			"POST /api/v2/lht/{id}/insert",
			"POST /api/v2/lht/{id}/approve",
			"POST /api/v2/lht/{id}/reject",
			"POST /api/v2/lht/{id}/reply",
		},
	})
}

// ────────────────────────────────────────────────────────────
// handleAction — POST /api/v2/lht/{id}/{action}
// ────────────────────────────────────────────────────────────

func (h *APIHandler) handleAction(w http.ResponseWriter, r *http.Request, goalID, action string) {
	// Logs is a GET endpoint, handle before the POST check.
	if action == "logs" {
		h.handleLogs(w, r, goalID)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	switch action {
	case "pause", "resume", "stop", "escalate", "approve", "reject":
		h.handleDispatch(w, r, goalID, action)
	case "insert":
		h.handleInsert(w, r, goalID)
	case "reply":
		h.handleReply(w, r, goalID)
	default:
		http.Error(w, fmt.Sprintf(`{"error":"unknown action: %s"}`, action), http.StatusBadRequest)
	}
}

// ────────────────────────────────────────────────────────────
// handleDispatch — pause/resume/stop/escalate/approve/reject
// ────────────────────────────────────────────────────────────

func (h *APIHandler) handleDispatch(w http.ResponseWriter, r *http.Request, goalID, cmd string) {
	// For approve/reject, parse body for reply content.
	var reply UserReply
	if cmd == "approve" || cmd == "reject" {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			reply = UserReply{
				ReplyType: cmd,
				Content:   body["content"],
			}
		}
	}

	args := []string{goalID}
	if err := h.engine.Dispatch(cmd, args, reply); err != nil {
		// Check if error is "not found".
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"action":  cmd,
		"goal_id": goalID,
	})
}

// ────────────────────────────────────────────────────────────
// handleInsert — insert a new requirement into a task.
// ────────────────────────────────────────────────────────────

func (h *APIHandler) handleInsert(w http.ResponseWriter, r *http.Request, goalID string) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	requirement, ok := body["requirement"]
	if !ok {
		http.Error(w, `{"error":"missing 'requirement' field"}`, http.StatusBadRequest)
		return
	}

	// Store the insertion as a log entry and notify via reply to the plan gate.
	msg := fmt.Sprintf("[INSERT] %s", requirement)
	_ = h.store.WriteLogEntry(goalID, "insert.log", []byte(msg+"\n"))

	// Attempt to reply via the plan gate (insert during planning/executing).
	err := h.engine.Reply("plan", goalID, "insert", requirement)
	if err != nil {
		// Fallback: try ground gate.
		err = h.engine.Reply("ground", goalID, "insert", requirement)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success":      true,
				"action":       "insert",
				"goal_id":      goalID,
				"logged":       true,
				"gate_reply":   "failed",
				"gate_reason":  err.Error(),
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"action":      "insert",
		"goal_id":     goalID,
		"requirement": requirement,
	})
}

// ────────────────────────────────────────────────────────────
// handleReply — reply to engine interaction gates.
// ────────────────────────────────────────────────────────────

func (h *APIHandler) handleReply(w http.ResponseWriter, r *http.Request, goalID string) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	gate := body["gate"]     // "ground", "plan", "final"
	action := body["action"] // "answer", "approve", "reject"
	content := body["content"]

	if gate == "" {
		http.Error(w, `{"error":"missing 'gate' field"}`, http.StatusBadRequest)
		return
	}
	if action == "" {
		http.Error(w, `{"error":"missing 'action' field"}`, http.StatusBadRequest)
		return
	}

	if err := h.engine.Reply(gate, goalID, action, content); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"action":  "reply",
		"gate":    gate,
		"goal_id": goalID,
	})
}

// ────────────────────────────────────────────────────────────
// handleLogs — GET /api/v2/lht/{id}/logs
// ────────────────────────────────────────────────────────────

func (h *APIHandler) handleLogs(w http.ResponseWriter, r *http.Request, goalID string) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Check goal exists.
	if _, err := h.store.LoadGoal(goalID); err != nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	page, size := parsePagination(r)
	logEntries := h.readLogEntries(goalID)

	// Paginate.
	total := len(logEntries)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}

	var pageEntries []map[string]interface{}
	if start < total {
		pageEntries = logEntries[start:end]
	}
	if pageEntries == nil {
		pageEntries = []map[string]interface{}{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries":  pageEntries,
		"page":     page,
		"size":     size,
		"total":    total,
		"goal_id":  goalID,
	})
}

// readLogEntries reads all log files in the goal's logs directory.
func (h *APIHandler) readLogEntries(goalID string) []map[string]interface{} {
	logsDir := h.store.LogsDir(goalID)
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return nil
	}

	var results []map[string]interface{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(logsDir, entry.Name()))
		if err != nil {
			continue
		}

		results = append(results, map[string]interface{}{
			"file":    entry.Name(),
			"content": string(data),
		})
	}

	return results
}

// ────────────────────────────────────────────────────────────
// helpers
// ────────────────────────────────────────────────────────────

// parsePagination extracts page and size from query params.
// Defaults: page=1, size=50. Clamped: 1≤page, 1≤size≤200.
func parsePagination(r *http.Request) (page, size int) {
	page, size = 1, 50

	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v >= 1 {
			page = v
		}
	}
	if s := r.URL.Query().Get("size"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			if v < 1 {
				v = 1
			} else if v > 200 {
				v = 200
			}
			size = v
		}
	}

	return page, size
}

// isValidGoalID is a basic sanity check for goal IDs.
func isValidGoalID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
