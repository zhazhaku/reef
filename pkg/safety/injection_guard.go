package safety

// ScanInput checks user input for prompt injection patterns.
// Returns the detected threat description, or empty string if clean.
//
// This is a defense-in-depth layer. It should be combined with:
//   - System prompt hardening (never reveal prompts, ignore contradictory instructions)
//   - Output content filtering (GAP-SS3)
//   - Tool access control via HermesGuard (GAP-SS4)
func ScanInput(text string) string {
	if text == "" {
		return ""
	}
	for _, p := range injectionPatterns {
		if p.MatchString(text) {
			return "Prompt injection pattern detected: " + p.String()
		}
	}
	return ""
}

// ScanInputWithDetails checks user input for prompt injection patterns.
// Returns a list of matched pattern descriptions.
func ScanInputWithDetails(text string) []string {
	if text == "" {
		return nil
	}
	var matches []string
	for _, p := range injectionPatterns {
		if p.MatchString(text) {
			matches = append(matches, p.String())
		}
	}
	return matches
}
