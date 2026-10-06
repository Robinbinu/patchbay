package proxy

import (
	"encoding/json"
	"testing"
)

// The Messages API has no extra_body field (that is a Python SDK option that
// merges into the body client-side); it must never reach the wire.
func TestAnthropicBodyHasNoExtraBody(t *testing.T) {
	in := `{"model":"claude-opus-5-5","max_tokens":1024,"thinking":{"type":"enabled","budget_tokens":512},"messages":[{"role":"user","content":"hi"}]}`
	var out map[string]any
	if err := json.Unmarshal(anthropicBody([]byte(in), "acct"), &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["extra_body"]; ok {
		t.Errorf("request body carries extra_body: %v", out["extra_body"])
	}
	if out["thinking"] == nil {
		t.Errorf("thinking config was dropped")
	}
}
