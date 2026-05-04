// Package safety provides security guardrails for agent I/O: prompt injection
// defense, output content scanning (PII, toxicity), and dangerous tool approval.
package safety

import "regexp"

// ---------------------------------------------------------------------------
// Prompt injection detection patterns
// ---------------------------------------------------------------------------

// injectionPatterns contains regex patterns for common prompt injection attacks.
// These are executed against user input before it reaches the LLM.
// OWASP LLM01: Prompt Injection — the #1 LLM application vulnerability.
var injectionPatterns = []*regexp.Regexp{
	// Direct override patterns
	regexp.MustCompile(`(?i)\bignore\s+(all\s+)?(previous|prior|above|earlier|system)\s+(instructions?|prompts?|messages?|context|directives?)\b`),
	regexp.MustCompile(`(?i)\byou\s+are\s+now\s+(DAN|jailbroken|unshackled|unfiltered|unrestricted)\b`),
	regexp.MustCompile(`(?i)\bpretend\s+(you\s+are|to\s+be|that)\b`),
	regexp.MustCompile(`(?i)\bact\s+as\s+(if\s+)?(you\s+are|a\s+different)\b`),
	regexp.MustCompile(`(?i)\byou\s+are\s+a\s+(different|new)\s+(AI|model|assistant|system)\b`),

	// System prompt extraction
	regexp.MustCompile(`(?i)\b(reveal|show|display|print|output|repeat|echo|tell\s+me)\s+(your\s+)?(system\s+(prompt|message|instruction|directive)|initial\s+prompt|base\s+prompt|hidden\s+prompt|secret\s+prompt)\b`),
	regexp.MustCompile(`(?i)\bwhat\s+(is|are|was)\s+(your\s+)?(system\s+(prompt|instruction|message)|initial\s+instruction|prompt\s+template)\b`),
	regexp.MustCompile(`(?i)\b(what|show|display)\s+(does\s+)?your\s+(prompt|system)\s+say\b`),

	// Delimiter injection
	regexp.MustCompile(`(-{3,}|_{3,}|={3,})\s*(END|END OF|SYSTEM|INSTRUCTIONS?|PROMPT|MESSAGE|CONTEXT)(\s*(END|OF|SYSTEM|INSTRUCTIONS?|PROMPT|MESSAGE|CONTEXT))*\s*(-{3,}|_{3,}|={3,})`),

	// Role manipulation
	regexp.MustCompile(`(?i)\bfrom\s+now\s+on\s+(you\s+(are|will\s+be|must)|your\s+role\s+is)\b`),
	regexp.MustCompile(`(?i)\bswitch\s+(your\s+)?(role|persona|identity|character)\b`),
	regexp.MustCompile(`(?i)\bdisregard\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|rules?|constraints?|limitations?|restrictions?|guidelines?)\b`),

	// Goal hijacking
	regexp.MustCompile(`(?i)\byour\s+(new|only|primary|main|sole)\s+(goal|objective|purpose|task|job|mission)\s+(is|now)\b`),
	regexp.MustCompile(`(?i)\bdo\s+not\s+(follow|obey|respect|adhere\s+to|comply\s+with)\s+(your\s+)?(system\s+)?(instructions?|prompts?|rules?|guidelines?)\b`),
	regexp.MustCompile(`(?i)\boverrid(e|ing)\s+(your\s+)?(system\s+)?(instructions?|prompts?|rules?|guidelines?)\b`),
}

// ---------------------------------------------------------------------------
// PII detection patterns
// ---------------------------------------------------------------------------

// piiPatterns contains regex patterns for sensitive data that should not be
// sent to users. These are executed against LLM output before delivery.
// OWASP LLM06: Sensitive Information Disclosure.
var piiPatterns = []struct {
	Name    string
	Pattern *regexp.Regexp
}{
	{
		Name:    "credit_card",
		Pattern: regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`),
	},
	{
		Name:    "ssn",
		Pattern: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
	},
	{
		Name:    "email",
		Pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
	},
	{
		Name:    "phone_cn",
		Pattern: regexp.MustCompile(`\b1[3-9]\d{9}\b`),
	},
	{
		Name:    "ip_private",
		Pattern: regexp.MustCompile(`\b(?:10\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])|192\.168)\.\d{1,3}\.\d{1,3}\b`),
	},
	{
		Name:    "api_key_pattern",
		Pattern: regexp.MustCompile(`(?i)\b(sk-[A-Za-z0-9]{32,}|AIza[A-Za-z0-9_-]{35}|sk-ant-[A-Za-z0-9_-]{32,})\b`),
	},
}
