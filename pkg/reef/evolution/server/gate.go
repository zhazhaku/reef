package server

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// GatekeeperStore — persistence interface required by the gatekeeper
// ---------------------------------------------------------------------------

// GatekeeperStore combines GeneStore methods (for Layer 2 dedup) with
// task query methods (for Layer 3 regression testing).
type GatekeeperStore interface {
	GeneStore
	// GetRecentTasks returns the most recent tasks for a given role.
	// Used by Layer 3 regression testing.
	GetRecentTasks(role string, limit int) ([]*reef.Task, error)
}

// ---------------------------------------------------------------------------
// LLMProvider — optional interface for Layer 3 regression testing
// ---------------------------------------------------------------------------

// LLMProvider abstracts the LLM chat interface required for Layer 3.
// When nil, Layer 3 is skipped even if enabled.
type LLMProvider interface {
	Chat(ctx context.Context, prompt string) (string, error)
}

// ---------------------------------------------------------------------------
// GatekeeperConfig
// ---------------------------------------------------------------------------

// GatekeeperConfig configures the 3-layer server-side gatekeeper.
type GatekeeperConfig struct {
	// EnableLayer1 controls the safety audit layer (regex pattern matching).
	// Default: true.
	EnableLayer1 bool

	// EnableLayer2 controls the deduplication layer (text similarity).
	// Default: true.
	EnableLayer2 bool

	// EnableLayer3 controls the regression test layer (LLM simulation).
	// Default: false (expensive, requires LLM).
	EnableLayer3 bool

	// DedupThreshold is the Jaccard similarity threshold above which a gene
	// is rejected as duplicate. Default: 0.80.
	DedupThreshold float64

	// DangerousPatterns is a list of regex patterns to detect dangerous operations.
	// When nil, DefaultDangerousPatterns() is used.
	DangerousPatterns []string

	// MaxRegressionSimulations is the maximum number of recent tasks to
	// simulate against in Layer 3 regression testing. Default: 3.
	MaxRegressionSimulations int
}

// DefaultGatekeeperConfig returns a GatekeeperConfig with sensible defaults.
func DefaultGatekeeperConfig() GatekeeperConfig {
	return GatekeeperConfig{
		EnableLayer1:             true,
		EnableLayer2:             true,
		EnableLayer3:             false,
		DedupThreshold:           0.80,
		DangerousPatterns:        nil, // will use DefaultDangerousPatterns
		MaxRegressionSimulations: 3,
	}
}

// setDefaults applies default values for zero-valued config fields.
func (c *GatekeeperConfig) setDefaults() {
	// Bool fields are set via explicit constructor; zero-value is "false" which is desired.
	// But we need defaults for numeric fields.
	if c.DedupThreshold <= 0 {
		c.DedupThreshold = 0.80
	}
	if c.MaxRegressionSimulations <= 0 {
		c.MaxRegressionSimulations = 3
	}
	if c.DangerousPatterns == nil {
		c.DangerousPatterns = DefaultDangerousPatterns()
	}
}

// ---------------------------------------------------------------------------
// DefaultDangerousPatterns — standard set of dangerous operations
// ---------------------------------------------------------------------------

// DefaultDangerousPatterns returns the built-in list of dangerous regex patterns
// for server-side safety audit (Layer 1). These patterns are matched
// case-insensitively against the ControlSignal and MatchCondition.
func DefaultDangerousPatterns() []string {
	return []string{
		// File system destruction
		`rm\s+-rf\s+/`,
		`rm\s+-rf\s+~`,
		`rm\s+-rf\s+\*`,
		// Privilege escalation
		`sudo\s+`,
		// Database destruction
		`DROP\s+(TABLE|DATABASE|INDEX)`,
		`DELETE\s+FROM`,
		`TRUNCATE(\s+TABLE)?`,
		// Disk formatting
		`mkfs\.`,
		`FORMAT\s+(C:|D:|/)`,
		// System shutdown
		`shutdown\s+(-h|-r|now)`,
		`reboot`,
		`halt`,   
		// Permission escalation
		`chmod\s+777`,
		`chmod\s+[0-7]*7[0-7]*7[0-7]*7`,
		// Command injection via curl-pipe
		`curl.*\|.*(ba)?sh`,
		`wget.*\|.*(ba)?sh`,
		// Code execution
		`\beval\b`,
		`\bexec\b`,
		`os\.system`,
		`subprocess\.`,
		`__import__\s*\(\s*['"]os['"]`,
		`exec\s*\(`,
		// Environment variable injection
		`\$\s*\(\s*`,
		"`[^`]*`",          // backtick eval
		// File overwrite outside workspace
		`>\s*/etc/`,
		`>\s*/root/`,
		`>\s*/boot/`,
		`>\s*/proc/`,
		`>\s*/sys/`,
		// Network bind to privileged port in bind/listen context
		`(bind|listen|socket).*:80\b`,
		`(bind|listen|socket).*:443\b`,
		`(bind|listen|socket).*:[0-9]{1,3}\b`, // any port binding in suspicious context
		// Fork bomb
		`:\(\)\{ :\|:& \};:`,
		// Device overwrite
		`>/dev/sd[a-z]`,
		`dd\s+if=.*of=/dev/`,
		// SSH key theft
		`~/.ssh/id_rsa`,
		`/etc/shadow`,
		// Dangerous systemctl/systemd operations
		`systemctl\s+(stop|disable|mask)`,
		// Piping to interpreters
		`\|\s*(python|perl|ruby|php|node)\b`,
	}
}

// ---------------------------------------------------------------------------
// Gatekeeper — concrete implementation of ServerGatekeeper
// ---------------------------------------------------------------------------

// Gatekeeper implements ServerGatekeeper with a 3-layer review pipeline:
// Layer 1 — Safety audit (deterministic regex, no LLM)
// Layer 2 — Deduplication (Jaccard 3-gram similarity)
// Layer 3 — Regression test (LLM simulation, harden strategy only)
type Gatekeeper struct {
	store           GatekeeperStore
	llm             LLMProvider
	config          GatekeeperConfig
	logger          *slog.Logger
	compiledPatterns []*regexp.Regexp
	rejectCallback  func(geneID string, result GateResult)
}

// NewGatekeeper creates a new Gatekeeper.
// Panics if any DangerousPattern fails to compile (configuration error).
func NewGatekeeper(store GatekeeperStore, llm LLMProvider, config GatekeeperConfig, logger *slog.Logger) *Gatekeeper {
	config.setDefaults()
	if logger == nil {
		logger = slog.Default()
	}

	gk := &Gatekeeper{
		store:  store,
		llm:    llm,
		config: config,
		logger: logger,
	}

	// Compile dangerous patterns at construction time
	if config.EnableLayer1 && len(config.DangerousPatterns) > 0 {
		gk.compiledPatterns = make([]*regexp.Regexp, 0, len(config.DangerousPatterns))
		for _, pat := range config.DangerousPatterns {
			re, err := regexp.Compile("(?i)" + pat)
			if err != nil {
				panic(fmt.Sprintf("Gatekeeper: failed to compile dangerous pattern %q: %v", pat, err))
			}
			gk.compiledPatterns = append(gk.compiledPatterns, re)
		}
	}

	// Warn if all layers disabled
	if !config.EnableLayer1 && !config.EnableLayer2 && !config.EnableLayer3 {
		gk.logger.Warn("all gatekeeper layers disabled — all genes will pass")
	}

	// Warn if Layer 3 enabled but no LLM
	if config.EnableLayer3 && llm == nil {
		gk.logger.Warn("Layer 3 enabled but no LLM provider — Layer 3 will be skipped")
	}

	return gk
}

// SetRejectionCallback sets a callback invoked on each rejection.
// Called synchronously from Review (not in a goroutine).
// Panics in the callback are recovered and logged.
func (g *Gatekeeper) SetRejectionCallback(cb func(geneID string, result GateResult)) {
	g.rejectCallback = cb
}

// Review implements ServerGatekeeper. It executes the 3-layer pipeline
// in sequence (fail-fast). Returns a GateResult and nil error on success,
// or an error if the review infrastructure itself fails.
func (g *Gatekeeper) Review(ctx context.Context, gene *evolution.Gene) (*GateResult, error) {
	// Edge case: all layers disabled — always pass
	if !g.config.EnableLayer1 && !g.config.EnableLayer2 && !g.config.EnableLayer3 {
		g.logger.Debug("all layers disabled, always pass",
			slog.String("gene_id", gene.ID))
		return &GateResult{Passed: true}, nil
	}

	// Layer 1: Safety Audit
	if g.config.EnableLayer1 {
		passed, reason := g.reviewLayer1(gene)
		if !passed {
			result := &GateResult{
				Passed:        false,
				Reason:        reason,
				RejectedLayer: 1,
			}
			g.invokeRejectCallback(gene.ID, *result)
			return result, nil
		}
	}

	// Layer 2: Deduplication
	var maxSimilarity float64
	if g.config.EnableLayer2 {
		passed, reason, similarity := g.reviewLayer2(ctx, gene)
		maxSimilarity = similarity
		if !passed {
			sim := similarity
			result := &GateResult{
				Passed:          false,
				Reason:          reason,
				RejectedLayer:   2,
				SimilarityScore: &sim,
			}
			g.invokeRejectCallback(gene.ID, *result)
			return result, nil
		}
	}

	// Layer 3: Regression Test (optional, harden strategy only)
	if g.config.EnableLayer3 {
		passed, reason, riskAssessment := g.reviewLayer3(ctx, gene)
		if !passed {
			result := &GateResult{
				Passed:          false,
				Reason:          reason,
				RejectedLayer:   3,
				RiskAssessment:  riskAssessment,
			}
			if maxSimilarity > 0 {
				result.SimilarityScore = &maxSimilarity
			}
			g.invokeRejectCallback(gene.ID, *result)
			return result, nil
		}
		// Layer 3 passed
		result := &GateResult{Passed: true}
		if riskAssessment != "" {
			result.RiskAssessment = riskAssessment
		}
		if maxSimilarity > 0 {
			result.SimilarityScore = &maxSimilarity
		}
		return result, nil
	}

	return &GateResult{Passed: true}, nil
}

// invokeRejectCallback safely calls the rejection callback, recovering panics.
func (g *Gatekeeper) invokeRejectCallback(geneID string, result GateResult) {
	if g.rejectCallback == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("rejection callback panicked",
				slog.String("gene_id", geneID),
				slog.Any("panic", r))
		}
	}()
	g.rejectCallback(geneID, result)
}

// ---------------------------------------------------------------------------
// Layer 1 — Safety Audit (deterministic regex, no LLM)
// ---------------------------------------------------------------------------

// reviewLayer1 checks the gene against dangerous pattern regexes.
// Returns (true, "") if clean, or (false, reason) if a dangerous pattern is found.
func (g *Gatekeeper) reviewLayer1(gene *evolution.Gene) (bool, string) {
	if !g.config.EnableLayer1 || len(g.compiledPatterns) == 0 {
		return true, ""
	}

	// Check ControlSignal
	for _, re := range g.compiledPatterns {
		if re.MatchString(gene.ControlSignal) {
			return false, fmt.Sprintf("dangerous pattern detected: %s", re.String())
		}
	}

	// Check MatchCondition
	for _, re := range g.compiledPatterns {
		if re.MatchString(gene.MatchCondition) {
			return false, fmt.Sprintf("dangerous pattern detected in match_condition: %s", re.String())
		}
	}

	return true, ""
}

// ---------------------------------------------------------------------------
// Layer 2 — Deduplication via Jaccard 3-gram similarity
// ---------------------------------------------------------------------------

// stopWords is a set of common English words excluded from word-frequency
// vectors used in the cosine fallback similarity metric.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"to": true, "of": true, "in": true, "for": true, "on": true,
	"with": true, "at": true, "by": true, "from": true, "as": true,
	"into": true, "through": true, "during": true, "before": true, "after": true,
	"above": true, "below": true, "between": true, "and": true, "but": true,
	"or": true, "not": true, "if": true, "then": true, "else": true,
	"when": true, "where": true, "why": true, "how": true, "all": true,
	"each": true, "every": true, "both": true, "few": true, "more": true,
	"most": true, "other": true, "some": true, "such": true, "no": true,
	"only": true, "own": true, "same": true, "so": true, "than": true,
	"too": true, "very": true, "just": true, "now": true,
}

// reviewLayer2 performs deduplication via Jaccard similarity on 3-gram tokenized text.
// Returns (true, "", 0.0) if unique, or (false, reason, similarity) if duplicate.
func (g *Gatekeeper) reviewLayer2(ctx context.Context, gene *evolution.Gene) (bool, string, float64) {
	if !g.config.EnableLayer2 {
		return true, "", 0.0
	}

	// Query existing approved genes
	existingGenes, err := g.store.GetApprovedGenes(gene.Role, 50)
	if err != nil {
		g.logger.Warn("Layer 2: store query failed, fail-open",
			slog.String("gene_id", gene.ID),
			slog.String("role", gene.Role),
			slog.String("error", err.Error()))
		return true, "", 0.0
	}

	if len(existingGenes) == 0 {
		return true, "", 0.0
	}

	gene3Grams := tokenize3Grams(gene.ControlSignal)
	if len(gene3Grams) == 0 {
		// Empty or very short control signal — cannot dedup, allow
		return true, "", 0.0
	}

	var maxSimilarity float64
	var closestGeneID string

	for _, existing := range existingGenes {
		if existing.ControlSignal == "" {
			continue
		}
		existing3Grams := tokenize3Grams(existing.ControlSignal)
		if len(existing3Grams) == 0 {
			continue
		}

		jaccard := jaccardSimilarity(gene3Grams, existing3Grams)
		if jaccard > maxSimilarity {
			maxSimilarity = jaccard
			closestGeneID = existing.ID
		}

		if jaccard >= g.config.DedupThreshold {
			return false, fmt.Sprintf("similar to existing gene %s (similarity=%.2f)", existing.ID, jaccard), jaccard
		}
	}

	// Fallback: if Jaccard is 0 but texts might still be similar via word frequency
	if maxSimilarity == 0.0 {
		for _, existing := range existingGenes {
			if existing.ControlSignal == "" {
				continue
			}
			cosine := cosineSimilarity(gene.ControlSignal, existing.ControlSignal)
			if cosine > maxSimilarity {
				maxSimilarity = cosine
				closestGeneID = existing.ID
			}
			if cosine >= g.config.DedupThreshold {
				return false, fmt.Sprintf("similar to existing gene %s (cosine=%.2f)", existing.ID, cosine), cosine
			}
		}
	}

	_ = closestGeneID // used for debugging, avoid unused warning
	return true, "", maxSimilarity
}

// tokenize3Grams splits text into a set of 3-word shingles.
// Text is lowercased and split on whitespace and punctuation.
func tokenize3Grams(text string) map[string]bool {
	cleaned := strings.ToLower(text)
	// Replace common punctuation with space
	replacer := strings.NewReplacer(
		".", " ", ",", " ", ";", " ", ":", " ", "!", " ", "?", " ",
		"(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ",
		"\"", " ", "'", " ", "`", " ", "\n", " ", "\r", " ", "\t", " ",
		"=", " ", "<", " ", ">", " ", "/", " ", "\\", " ", "|", " ",
		"&", " ", "*", " ", "#", " ", "@", " ", "$", " ", "%", " ",
		"^", " ", "~", " ", "+", " ", "-", " ", "_", " ",
	)
	cleaned = replacer.Replace(cleaned)

	// Split into words
	words := strings.Fields(cleaned)
	if len(words) < 3 {
		return nil
	}

	ngrams := make(map[string]bool, len(words)-2)
	for i := 0; i <= len(words)-3; i++ {
		gram := words[i] + " " + words[i+1] + " " + words[i+2]
		ngrams[gram] = true
	}
	return ngrams
}

// jaccardSimilarity computes the Jaccard similarity between two sets of 3-grams.
func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0 // both empty → identical
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	return float64(intersection) / float64(union)
}

// cosineSimilarity computes a simplified cosine similarity on word frequency vectors
// with stop-word filtering. Used as a fallback when Jaccard produces 0 matches.
func cosineSimilarity(a, b string) float64 {
	vecA := wordFreqVector(a)
	vecB := wordFreqVector(b)

	if len(vecA) == 0 || len(vecB) == 0 {
		return 0.0
	}

	// Compute dot product and magnitudes using the union of keys
	dotProduct := 0.0
	magA := 0.0
	magB := 0.0

	// Process keys from vecA
	for k, va := range vecA {
		vb := vecB[k]
		dotProduct += va * vb
		magA += va * va
	}

	// Process keys only in vecB
	for k, vb := range vecB {
		if _, ok := vecA[k]; !ok {
			magB += vb * vb
		} else {
			// Already counted mag contribution in first loop
			magB += vb * vb
		}
	}

	// Actually, we need to sum all mag contributions consistently.
	// Let's recompute properly:
	allKeys := make(map[string]bool)
	for k := range vecA {
		allKeys[k] = true
	}
	for k := range vecB {
		allKeys[k] = true
	}

	dotProduct = 0.0
	magA = 0.0
	magB = 0.0
	for k := range allKeys {
		va := vecA[k]
		vb := vecB[k]
		dotProduct += va * vb
		magA += va * va
		magB += vb * vb
	}

	if magA == 0 || magB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(magA) * math.Sqrt(magB))
}

// wordFreqVector builds a TF (term frequency) vector from text,
// excluding stop words.
func wordFreqVector(text string) map[string]float64 {
	cleaned := strings.ToLower(text)
	cleaned = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			return r
		}
		return ' '
	}, cleaned)

	words := strings.Fields(cleaned)
	if len(words) == 0 {
		return nil
	}

	vector := make(map[string]float64)
	for _, w := range words {
		if stopWords[w] {
			continue
		}
		if len(w) <= 1 {
			continue
		}
		vector[w] += 1.0
	}

	return vector
}

// ---------------------------------------------------------------------------
// Layer 3 — Regression Test via LLM simulation
// ---------------------------------------------------------------------------

// reviewLayer3 performs regression testing via LLM simulation.
// Only runs for harden strategy. Returns (true, "", assessment) if safe,
// or (false, reason, assessment) if the gene would cause regressions.
func (g *Gatekeeper) reviewLayer3(ctx context.Context, gene *evolution.Gene) (bool, string, string) {
	// Skip if disabled
	if !g.config.EnableLayer3 {
		return true, "", ""
	}

	// Skip if no LLM
	if g.llm == nil {
		g.logger.Warn("Layer 3: enabled but no LLM provider, skip",
			slog.String("gene_id", gene.ID))
		return true, "", ""
	}

	// Only harden strategy triggers regression tests
	if gene.StrategyName != string(evolution.StrategyHarden) {
		return true, "", ""
	}

	// Query recent tasks
	tasks, err := g.store.GetRecentTasks(gene.Role, g.config.MaxRegressionSimulations)
	if err != nil {
		g.logger.Warn("Layer 3: failed to get recent tasks, fail-open",
			slog.String("gene_id", gene.ID),
			slog.String("role", gene.Role),
			slog.String("error", err.Error()))
		return true, "", ""
	}

	if len(tasks) == 0 {
		// No recent tasks, nothing to simulate against
		return true, "", ""
	}

	// Build LLM prompt
	prompt := buildRegressionPrompt(gene, tasks, 4000)

	// Call LLM with timeout
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	response, err := g.llm.Chat(ctx, prompt)
	if err != nil {
		g.logger.Warn("Layer 3: LLM call failed, fail-open",
			slog.String("gene_id", gene.ID),
			slog.String("error", err.Error()))
		return true, "", ""
	}

	// Parse response
	response = strings.TrimSpace(response)
	upper := strings.ToUpper(response)

	if strings.HasPrefix(upper, "PASS") {
		assessment := strings.TrimSpace(strings.TrimPrefix(response, "PASS"))
		assessment = strings.TrimSpace(strings.TrimPrefix(assessment, "pass"))
		// Remove leading colon/space
		assessment = strings.TrimLeft(assessment, ": \t\n")
		return true, "", assessment
	}

	if strings.HasPrefix(upper, "FAIL") {
		reason := strings.TrimPrefix(response, "FAIL")
		reason = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(reason), "fail"))
		reason = strings.TrimLeft(reason, ": \t\n")
		if reason == "" {
			reason = "regression risk detected"
		}
		return false, reason, reason
	}

	// Ambiguous response
	return false, "ambiguous_llm_response", response
}

// buildRegressionPrompt constructs the LLM prompt for regression testing.
// Ensures total prompt is under maxTokens (estimated by character count / 2).
func buildRegressionPrompt(gene *evolution.Gene, tasks []*reef.Task, maxTokens int) string {
	var sb strings.Builder

	// Gene summary (max 2000 chars)
	geneText := gene.ControlSignal
	if len(geneText) > 2000 {
		geneText = geneText[:2000]
	}

	sb.WriteString("You are evaluating a new Gene for safety.\n\n")
	sb.WriteString("Gene ControlSignal:\n")
	sb.WriteString(geneText)
	sb.WriteString("\n\n")

	// Task summaries
	sb.WriteString("Recent task histories:\n")

	// Estimate tokens: ~2 chars per token
	maxPromptChars := maxTokens * 2
	currentChars := len(sb.String())
	remainingChars := maxPromptChars - currentChars - 200 // buffer

	taskCount := 0
	for _, task := range tasks {
		taskSummary := buildTaskSummary(task)
		if remainingChars-len(taskSummary) < 0 {
			break
		}
		sb.WriteString(taskSummary)
		remainingChars -= len(taskSummary)
		taskCount++
	}

	if taskCount == 0 {
		return ""
	}

	sb.WriteString("\nFor each task, determine if applying this Gene would cause regressions ")
	sb.WriteString("(break previously working behavior, introduce new failures, or violate existing constraints).\n\n")
	sb.WriteString("Respond with 'PASS' if safe or 'FAIL: {reason}' if risky.")

	return sb.String()
}

// buildTaskSummary creates a compact summary of a task for the LLM prompt.
func buildTaskSummary(task *reef.Task) string {
	instruction := task.Instruction
	if len(instruction) > 200 {
		instruction = instruction[:200]
	}
	summary := fmt.Sprintf("- Task %s [%s]: %s\n", task.ID, task.Status, instruction)
	if task.Result != nil && task.Result.Text != "" {
		resultText := task.Result.Text
		if len(resultText) > 100 {
			resultText = resultText[:100]
		}
		summary += fmt.Sprintf("  Result: %s\n", resultText)
	}
	if task.Error != nil {
		summary += fmt.Sprintf("  Error: %s\n", task.Error.Message)
	}
	return summary
}
