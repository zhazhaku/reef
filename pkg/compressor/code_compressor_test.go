package compressor

import (
	"context"
	"strings"
	"testing"
)

// =============================================================================
// [RED] CodeCompressor tests — will FAIL because CodeCompressor does not exist.
// =============================================================================

// =============================================================================
// TestCodeCompressorRegister — verify Name() and Type()
// =============================================================================

func TestCodeCompressorRegister(t *testing.T) {
	cc := NewCodeCompressor()
	if cc == nil {
		t.Fatal("NewCodeCompressor() returned nil")
	}

	Register("code_compressor", cc)

	got := Get("code_compressor")
	if got == nil {
		t.Fatal("Get('code_compressor') returned nil after Register")
	}

	if got.Name() != "code_compressor" {
		t.Errorf("Name() = %q, want %q", got.Name(), "code_compressor")
	}

	if got.Type() != CompressTypeTruncate {
		t.Errorf("Type() = %v, want %v", got.Type(), CompressTypeTruncate)
	}
}

// =============================================================================
// TestCodeCompressorCompress — basic code compression returns valid result
// =============================================================================

func TestCodeCompressorCompress(t *testing.T) {
	cc := NewCodeCompressor()

	input := []byte("func hello() {\n\tfmt.Println(\"world\")\n}\n")
	result, err := cc.Compress(context.Background(), input, CompressOptions{
		MaxTokens: 4096,
		Priority:  "normal",
	})

	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if result == nil {
		t.Fatal("Compress returned nil result")
	}

	// Verify result fields
	if result.OriginalSz == 0 {
		t.Error("OriginalSz should be non-zero")
	}
	if result.Ratio <= 0 || result.Ratio > 1.0 {
		t.Errorf("Ratio = %f, want 0 < ratio <= 1.0", result.Ratio)
	}

	// Should preserve func name
	if !strings.Contains(result.Content, "hello") {
		t.Error("Content should preserve function name 'hello'")
	}
}

// =============================================================================
// TestCodeCompressorCodeBlock — preserves code block markers
// =============================================================================

func TestCodeCompressorCodeBlock(t *testing.T) {
	cc := NewCodeCompressor()

	input := []byte("```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n")
	result, err := cc.Compress(context.Background(), input, CompressOptions{
		MaxTokens: 4096,
		Priority:  "normal",
	})

	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// Code block markers should be preserved
	if !strings.Contains(result.Content, "```") {
		t.Error("Content should preserve code block markers")
	}

	// Function content should be preserved
	if !strings.Contains(result.Content, "main") {
		t.Error("Content should preserve function name 'main'")
	}
}

// =============================================================================
// TestCodeCompressorLongLines — long lines get truncated
// =============================================================================

func TestCodeCompressorLongLines(t *testing.T) {
	cc := NewCodeCompressor()

	// Build code with a very long line
	shortLines := "package test\nimport \"fmt\"\n"
	longLine := "var x = \"" + strings.Repeat("abcdefghij", 50) + "\" // very long line\n" // ~500 chars
	input := []byte(shortLines + longLine)

	result, err := cc.Compress(context.Background(), input, CompressOptions{
		MaxTokens: 4096,
		Priority:  "low",
	})

	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// The very long line should be truncated
	content := result.Content
	if strings.Count(content, "abcdefghij") >= 40 {
		t.Error("Long line should be truncated, but found too many repetitions")
	}

	// Short lines should be preserved
	if !strings.Contains(content, "package test") {
		t.Error("Short line 'package test' should be preserved")
	}
}

// =============================================================================
// TestCodeCompressorEmptyContent — empty content handled gracefully
// =============================================================================

func TestCodeCompressorEmptyContent(t *testing.T) {
	cc := NewCodeCompressor()

	emptyInputs := []string{
		"",
		"   \n\t  ",
	}

	for _, input := range emptyInputs {
		result, err := cc.Compress(context.Background(), []byte(input), CompressOptions{
			MaxTokens: 4096,
			Priority:  "normal",
		})
		if err != nil {
			t.Errorf("Compress should not error on empty input %q: %v", input, err)
			continue
		}
		if result == nil {
			t.Errorf("Compress on empty input %q returned nil result", input)
		}
	}
}

// =============================================================================
// TestCodeCompressorPreserveImports — imports/packages preserved when possible
// =============================================================================

func TestCodeCompressorPreserveImports(t *testing.T) {
	cc := NewCodeCompressor()

	input := []byte(`package main

import (
	"fmt"
	"strings"
	"os"
)

func longFunctionNameThatDoesSomethingComplex() {
	// 50 lines of fake code
	for i := 0; i < 100; i++ {
		fmt.Println("line", i)
	}
	for i := 0; i < 100; i++ {
		fmt.Println("line", i)
	}
	for i := 0; i < 100; i++ {
		fmt.Println("line", i)
	}
}
`)

	result, err := cc.Compress(context.Background(), input, CompressOptions{
		MaxTokens: 1024,
		Priority:  "normal",
	})

	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// Import statements and package declaration should be preserved
	if !strings.Contains(result.Content, "package main") {
		t.Error("package declaration should be preserved")
	}
	if !strings.Contains(result.Content, "import") {
		t.Error("import keyword should be preserved")
	}
	if !strings.Contains(result.Content, "\"fmt\"") {
		t.Error("fmt import should be preserved")
	}

	// Function signature should be preserved
	if !strings.Contains(result.Content, "longFunctionNameThatDoesSomethingComplex") {
		t.Error("function signature should be preserved")
	}
}

// =============================================================================
// BenchmarkCodeCompressor — benchmark skeleton
// =============================================================================

func BenchmarkCodeCompressor(b *testing.B) {
	b.ReportAllocs()

	cc := NewCodeCompressor()
	input := []byte(`package main

import "fmt"

func calculate(x, y int) int {
	result := x + y
	for i := 0; i < 10; i++ {
		result += i
	}
	return result
}
`)
	opts := CompressOptions{MaxTokens: 4096, Priority: "normal"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cc.Compress(context.Background(), input, opts)
	}
}
