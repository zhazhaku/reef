package client

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// GateConfig configures the LocalGatekeeper's two-layer validation pipeline.
type GateConfig struct {
	// SyntaxMaxChars is the max ControlSignal character length. Default 5000.
	SyntaxMaxChars int

	// SyntaxMaxLines is the max ControlSignal line count. Default 200.
	SyntaxMaxLines int

	// SyntaxMaxWarnings is the max FailureWarnings count. Default 20.
	SyntaxMaxWarnings int

	// EnableSemanticCheck controls whether Layer 2 (dangerous pattern detection)
	// is executed. Default true.
	EnableSemanticCheck bool

	// DangerousPatterns is a list of regex patterns to detect dangerous commands
	// in the ControlSignal or MatchCondition. Patterns are matched case-insensitively.
	// When empty, Layer 2 is skipped.
	DangerousPatterns []string
}

// DefaultDangerousPatterns returns the built-in list of dangerous regex patterns.
func DefaultDangerousPatterns() []string {
	return []string{
		`rm\s+-rf\s+/`,
		`sudo\s+`,
		`DROP\s+(TABLE|DATABASE)`,
		`DELETE\s+FROM`,
		`TRUNCATE(\s+TABLE)?`,
		`chmod\s+777`,
		`:\(\)\{ :\|:& \};:`,
		`mkfs\.`,
		`shutdown\s+(-h|-r|now)`,
		`>/dev/sda`,
		`curl.*\|.*(ba)?sh`,
	}
}

// setDefaults applies default values for zero-valued config fields.
func (c *GateConfig) setDefaults() {
	if c.SyntaxMaxChars <= 0 {
		c.SyntaxMaxChars = 5000
	}
	if c.SyntaxMaxLines <= 0 {
		c.SyntaxMaxLines = 200
	}
	if c.SyntaxMaxWarnings <= 0 {
		c.SyntaxMaxWarnings = 20
	}
	if c.DangerousPatterns == nil {
		c.DangerousPatterns = DefaultDangerousPatterns()
	}
	// Note: EnableSemanticCheck must be set explicitly by the caller.
	// When creating a GateConfig, set EnableSemanticCheck: true to enable
	// Layer 2 dangerous pattern detection.
}

// LocalGatekeeper performs client-side gene validation in two layers:
// Layer 1 — syntax (non-empty fields, length/line bounds)
// Layer 2 — semantics (dangerous pattern detection via case-insensitive regex)
type LocalGatekeeper struct {
	config           GateConfig
	logger           *slog.Logger
	compiledPatterns []*regexp.Regexp
}

// NewGatekeeper creates a new LocalGatekeeper.
// Panics if any DangerousPattern fails to compile (configuration error).
func NewGatekeeper(config GateConfig, logger *slog.Logger) *LocalGatekeeper {
	config.setDefaults()
	if logger == nil {
		logger = slog.Default()
	}

	gk := &LocalGatekeeper{
		config: config,
		logger: logger,
	}

	if config.EnableSemanticCheck && len(config.DangerousPatterns) > 0 {
		gk.compiledPatterns = make([]*regexp.Regexp, 0, len(config.DangerousPatterns))
		for _, pat := range config.DangerousPatterns {
			re, err := regexp.Compile("(?i)" + pat)
			if err != nil {
				// Configuration error: panic at init time
				panic(fmt.Sprintf("LocalGatekeeper: failed to compile dangerous pattern %q: %v", pat, err))
			}
			gk.compiledPatterns = append(gk.compiledPatterns, re)
		}
	}

	return gk
}

// Check implements GeneGateChecker. It returns true if the gene passes both
// Layer 1 (syntax) and Layer 2 (semantics, if enabled).
func (g *LocalGatekeeper) Check(gene *evolution.Gene) bool {
	pass, reason := g.CheckWithReason(gene)
	if !pass {
		g.logger.Warn("gene rejected by local gatekeeper",
			slog.String("gene_id", gene.ID),
			slog.String("reason", reason))
	}
	return pass
}

// CheckWithReason performs validation and returns (pass, reason).
// When pass is true, reason is empty.
func (g *LocalGatekeeper) CheckWithReason(gene *evolution.Gene) (bool, string) {
	if gene == nil {
		return false, "syntax: gene is nil"
	}

	// Layer 1: Syntax check (always runs)
	if reason := g.checkSyntax(gene); reason != "" {
		return false, reason
	}

	// Layer 2: Semantic check (configurable)
	if g.config.EnableSemanticCheck && len(g.compiledPatterns) > 0 {
		if reason := g.checkSemantics(gene); reason != "" {
			return false, reason
		}
	}

	return true, ""
}

// checkSyntax performs Layer 1 validation.
// Returns an empty string on success, or a rejection reason on failure.
func (g *LocalGatekeeper) checkSyntax(gene *evolution.Gene) string {
	if gene.StrategyName == "" {
		return "syntax: strategy_name must be non-empty"
	}
	if gene.ControlSignal == "" {
		return "syntax: control_signal must be non-empty"
	}
	if len(gene.ControlSignal) > g.config.SyntaxMaxChars {
		return fmt.Sprintf("syntax: control_signal exceeds %d chars", g.config.SyntaxMaxChars)
	}
	lc := lineCount(gene.ControlSignal)
	if lc > g.config.SyntaxMaxLines {
		return fmt.Sprintf("syntax: control_signal exceeds %d lines", g.config.SyntaxMaxLines)
	}
	if len(gene.FailureWarnings) > g.config.SyntaxMaxWarnings {
		return fmt.Sprintf("syntax: failure_warnings exceeds %d items", g.config.SyntaxMaxWarnings)
	}
	return ""
}

// checkSemantics performs Layer 2 validation: dangerous pattern detection.
// Returns an empty string on success, or a rejection reason on first match.
func (g *LocalGatekeeper) checkSemantics(gene *evolution.Gene) string {
	for _, re := range g.compiledPatterns {
		// Test against ControlSignal
		if re.MatchString(gene.ControlSignal) {
			return fmt.Sprintf("semantics: dangerous pattern detected: %s", re.String())
		}
		// Test against MatchCondition (prevent injection through condition field)
		if re.MatchString(gene.MatchCondition) {
			return fmt.Sprintf("semantics: dangerous pattern detected in match_condition: %s", re.String())
		}
	}
	return ""
}

// lineCount returns the number of lines in s.
// An empty string is treated as 1 line.
func lineCount(s string) int {
	return strings.Count(s, "\n") + 1
}
