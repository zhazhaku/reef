package feishu

import (
	"encoding/json"
	"testing"
)

func TestBuildDivExtraCardJSON(t *testing.T) {
	result, err := buildDivExtraCard("**结果**：这是回复内容\n\n分析完成。", "1. 首先分析了代码结构\n2. 检查了数据流\n3. 推断需要重启服务")
	if err != nil {
		t.Fatalf("buildDivExtraCard failed: %v", err)
	}

	// Parse back to validate
	var m map[string]any
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	schema := m["schema"].(string)
	if schema != "2.0" {
		t.Errorf("Expected schema 2.0, got %s", schema)
	}

	body := m["body"].(map[string]any)
	elements := body["elements"].([]any)
	if len(elements) != 2 {
		t.Fatalf("Expected 2 elements, got %d", len(elements))
	}

	// Both elements should be markdown
	e0 := elements[0].(map[string]any)
	if e0["tag"] != "markdown" {
		t.Errorf("First element tag should be markdown, got %s", e0["tag"])
	}

	e1 := elements[1].(map[string]any)
	if e1["tag"] != "markdown" {
		t.Errorf("Second element tag should be markdown, got %s", e1["tag"])
	}
	// Second markdown should contain thinking process separator and label
	content1 := e1["content"].(string)
	if content1 == "" {
		t.Error("Second element should have non-empty content")
	}

	t.Logf("Card JSON:\n%s", result)
}

func TestBuildDivExtraCardNoThought(t *testing.T) {
	// When thought is empty, should only have markdown element
	result, err := buildDivExtraCard("hello", "")
	if err != nil {
		t.Fatalf("buildDivExtraCard failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	body := m["body"].(map[string]any)
	elements := body["elements"].([]any)
	if len(elements) != 1 {
		t.Errorf("Expected 1 element when thought is empty, got %d", len(elements))
	}
}
