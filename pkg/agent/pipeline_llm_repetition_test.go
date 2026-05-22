package agent

import "testing"

func TestDetectRepetitionLoop(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "no repetition",
			content:  "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10",
			expected: false,
		},
		{
			name:     "8 repeated lines",
			content:  "Let me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\n",
			expected: true,
		},
		{
			name:     "6 repeated lines (below threshold)",
			content:  "A\nA\nA\nA\nA\nA\n",
			expected: false,
		},
		{
			name:     "7 repeated lines (boundary, triggers)",
			content:  "A\nA\nA\nA\nA\nA\nA\n",
			expected: true,
		},
		{
			name:     "10 repeated lines (above threshold)",
			content:  "B\nB\nB\nB\nB\nB\nB\nB\nB\nB\n",
			expected: true,
		},
		{
			name:     "repetition with blank lines interspersed",
			content:  "C\n\nC\n\nC\n\nC\n\nC\n\nC\n\nC\n\nC\n\nC\n\n",
			expected: true, // 9 non-empty lines of "C"
		},
		{
			name:     "repetition with different lines between",
			content:  "A\nB\nA\nB\nA\nB\nA\nB\nA\nB\nA\nB\nA\nB\n",
			expected: false, // alternating, not consecutive
		},
		{
			name:     "short content",
			content:  "hello\nworld\n",
			expected: false,
		},
		{
			name:     "empty content",
			content:  "",
			expected: false,
		},
		{
			name:     "realistic LLM repetition (50+ repeats)",
			content:  "Let me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\nLet me try to read the SKILL.md\n",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectRepetitionLoop(tt.content)
			if got != tt.expected {
				t.Errorf("detectRepetitionLoop() = %v, want %v", got, tt.expected)
			}
		})
	}
}
