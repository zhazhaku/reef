package safety

import "strings"

// ScanOutput checks LLM output for PII and sensitive content patterns.
// Returns the matched PII category name, or empty string if clean.
//
// When a match is found, the caller should decide whether to:
//   a) Block the response entirely (for high-sensitivity matches)
//   b) Redact the sensitive content and continue
//   c) Log a warning and allow through (for monitoring mode)
func ScanOutput(text string) string {
	if text == "" {
		return ""
	}
	for _, p := range piiPatterns {
		if p.Pattern.MatchString(text) {
			return p.Name
		}
	}
	return ""
}

// ScanOutputWithDetails checks LLM output for PII and sensitive content patterns.
// Returns a list of matched PII category names.
func ScanOutputWithDetails(text string) []string {
	if text == "" {
		return nil
	}
	var matches []string
	for _, p := range piiPatterns {
		if p.Pattern.MatchString(text) {
			matches = append(matches, p.Name)
		}
	}
	return matches
}

// RedactOutput replaces detected PII patterns with placeholder text.
// Returns the redacted text and whether any redactions were performed.
func RedactOutput(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	redacted := text
	changed := false
	for _, p := range piiPatterns {
		if p.Pattern.MatchString(redacted) {
			redacted = p.Pattern.ReplaceAllStringFunc(redacted, func(match string) string {
				return "[" + strings.ToUpper(p.Name) + "_REDACTED]"
			})
			changed = true
		}
	}
	return redacted, changed
}
