package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildThinkingCard(t *testing.T) {
	result, err := buildThinkingCard("正在分析需求...\n正在搜索资料...")
	if err != nil {
		t.Fatalf("buildThinkingCard failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Check schema
	if m["schema"] != "2.0" {
		t.Errorf("Expected schema 2.0, got %v", m["schema"])
	}

	// Check header
	header, ok := m["header"].(map[string]any)
	if !ok {
		t.Fatal("Missing header")
	}
	title, ok := header["title"].(map[string]any)
	if !ok {
		t.Fatal("Missing header.title")
	}
	if title["content"] != "💭 思考过程" {
		t.Errorf("Expected header title '💭 思考过程', got %v", title["content"])
	}
	if header["template"] != "blue" {
		t.Errorf("Expected blue template, got %v", header["template"])
	}

	// Check body has markdown with reasoning
	body := m["body"].(map[string]any)
	elements := body["elements"].([]any)
	if len(elements) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(elements))
	}
	e0 := elements[0].(map[string]any)
	if e0["tag"] != "markdown" {
		t.Errorf("Expected markdown element, got %s", e0["tag"])
	}
	if !strings.Contains(e0["content"].(string), "正在分析需求") {
		t.Errorf("Content missing reasoning text")
	}

	t.Logf("Thinking card JSON:\n%s", result)
}

func TestBuildFinalCard(t *testing.T) {
	answer := "**最终回复**\n\n分析结果：没有问题。"
	reasoning := "第一步检查了配置，第二步检查了代码，第三步验证了数据流。"

	result, err := buildFinalCard(answer, reasoning)
	if err != nil {
		t.Fatalf("buildFinalCard failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	if m["schema"] != "2.0" {
		t.Errorf("Expected schema 2.0, got %v", m["schema"])
	}

	body := m["body"].(map[string]any)
	elements := body["elements"].([]any)

	// Should have: markdown (answer), markdown (reasoning summary)
	if len(elements) != 2 {
		t.Fatalf("Expected 2 elements (markdown, markdown), got %d", len(elements))
	}

	e0 := elements[0].(map[string]any)
	if e0["tag"] != "markdown" {
		t.Errorf("First element should be markdown (answer), got %s", e0["tag"])
	}
	if e0["content"] != answer {
		t.Errorf("Answer content mismatch")
	}

	e1 := elements[1].(map[string]any)
	if e1["tag"] != "markdown" {
		t.Errorf("Second element should be markdown (reasoning), got %s", e1["tag"])
	}
	if !strings.Contains(e1["content"].(string), "💭") || !strings.Contains(e1["content"].(string), "思考过程") {
		t.Errorf("Second element missing thinking label, got: %s", e1["content"].(string))
	}

	t.Logf("Final card JSON:\n%s", result)
}

func TestBuildFinalCardNoReasoning(t *testing.T) {
	result, err := buildFinalCard("just answer", "")
	if err != nil {
		t.Fatalf("buildFinalCard failed: %v", err)
	}

	var m map[string]any
	json.Unmarshal([]byte(result), &m)

	body := m["body"].(map[string]any)
	elements := body["elements"].([]any)

	// No reasoning → only markdown element
	if len(elements) != 1 {
		t.Errorf("Expected 1 element when no reasoning, got %d", len(elements))
	}
}

func TestBuildThinkingCardTruncation(t *testing.T) {
	// Build a very long reasoning string
	longReasoning := strings.Repeat("这是一个很长的思考过程文本。", 300) // ~4500 runes

	result, err := buildThinkingCard(longReasoning)
	if err != nil {
		t.Fatalf("buildThinkingCard failed: %v", err)
	}

	var m map[string]any
	json.Unmarshal([]byte(result), &m)

	body := m["body"].(map[string]any)
	elements := body["elements"].([]any)
	content := elements[0].(map[string]any)["content"].(string)

	runes := []rune(content)
	if len(runes) > 4500 {
		t.Errorf("Thinking content should be truncated to 4500 runes, got %d", len(runes))
	}
}
