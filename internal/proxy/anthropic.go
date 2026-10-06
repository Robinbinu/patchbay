package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
)

const (
	ccVersion    = "2.1.217"
	billingSalt  = "59cf53e54c78"
	ccEntrypoint = "sdk-cli"

	stainlessPkgVersion  = "0.81.0"
	stainlessNodeVersion = "v24.3.0"
)

var ccOAuthBetas = []string{
	"oauth-2025-04-20",
	"interleaved-thinking-2025-05-14",
	"prompt-caching-scope-2026-01-05",
	"context-management-2025-06-27",
	"advisor-tool-2026-03-01",
	"thinking-token-count-2026-05-13",
	"extended-cache-ttl-2025-04-11",
}

func computeBillingHeader(body []byte) string {
	text := firstUserMessageText(body)
	version := ccVersion + "." + versionIntegrityHash(text)
	cch := contentHash(text)
	return fmt.Sprintf(
		"x-anthropic-billing-header: cc_version=%s; cc_entrypoint=%s; cch=%s;",
		version, ccEntrypoint, cch,
	)
}

func firstUserMessageText(body []byte) string {
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return ""
	}
	for _, msg := range req.Messages {
		if msg.Role != "user" {
			continue
		}
		var str string
		if json.Unmarshal(msg.Content, &str) == nil && str != "" {
			return str
		}
		var blocks []textBlock
		if json.Unmarshal(msg.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					return b.Text
				}
			}
		}
		return ""
	}
	return ""
}

func contentHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])[:5]
}

func versionIntegrityHash(text string) string {
	sampled := make([]byte, 0, 3)
	for _, i := range []int{4, 7, 20} {
		if i < len(text) {
			sampled = append(sampled, text[i])
		} else {
			sampled = append(sampled, '0')
		}
	}
	input := billingSalt + string(sampled) + ccVersion
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])[:3]
}

func anthropicBody(body []byte, accountUUID string) []byte {
	billingValue := computeBillingHeader(body)

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	messages, _ := req["messages"].([]any)
	if len(messages) == 0 {
		return body
	}

	rawSystem := req["system"]
	var system []any
	switch s := rawSystem.(type) {
	case string:
		if s != "" {
			system = []any{map[string]any{"type": "text", "text": s}}
		}
	case []any:
		system = s
	default:
		system = nil
	}

	kept := make([]any, 0, len(system)+2)
	movedTexts := make([]string, 0)
	identitySeen := false

	const identity = "You are Claude Code, Anthropic's official CLI for Claude."

	for _, entry := range system {
		block, ok := entry.(map[string]any)
		if !ok {
			kept = append(kept, entry)
			continue
		}
		typ, _ := block["type"].(string)
		if typ != "text" {
			kept = append(kept, block)
			continue
		}
		text, _ := block["text"].(string)
		if strings.HasPrefix(text, "x-anthropic-billing-header") {
			continue
		}
		if strings.HasPrefix(text, identity) {
			if identitySeen {
				continue
			}
			identitySeen = true
			rest := strings.TrimPrefix(text, identity)
			rest = strings.TrimLeft(rest, "\n")
			kept = append(kept, map[string]any{
				"type": "text",
				"text": identity,
				"cache_control": map[string]any{
					"type": "ephemeral",
					"ttl":  "1h",
				},
			})
			if rest != "" {
				movedTexts = append(movedTexts, rest)
			}
			continue
		}
		if text != "" {
			movedTexts = append(movedTexts, text)
		}
	}

	if !identitySeen {
		kept = append([]any{map[string]any{
			"type": "text",
			"text": identity,
			"cache_control": map[string]any{
				"type": "ephemeral",
				"ttl":  "1h",
			},
		}}, kept...)
	}

	billingEntry := map[string]any{
		"type": "text",
		"text": billingValue,
	}
	req["system"] = append([]any{billingEntry}, kept...)

	if len(movedTexts) > 0 {
		prependUserReminder(messages, movedTexts)
		req["messages"] = messages
	}

	if accountUUID != "" {
		deviceID := sha256Hex(fmt.Sprintf("patchbay-device-%s", accountUUID))
		userIDJSON, _ := json.Marshal(map[string]string{
			"device_id":    deviceID,
			"account_uuid": accountUUID,
			"session_id":   sessionID(),
		})
		existingMeta, _ := req["metadata"].(map[string]any)
		if existingMeta == nil {
			existingMeta = map[string]any{}
		}
		if _, ok := existingMeta["user_id"]; !ok {
			existingMeta["user_id"] = string(userIDJSON)
			req["metadata"] = existingMeta
		}
	}

	out, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return out
}

func prependUserReminder(messages []any, texts []string) {
	if len(texts) == 0 {
		return
	}
	combined := ""
	for _, t := range texts {
		combined += fmt.Sprintf("<system-reminder>\n%s\n</system-reminder>\n\n", t)
	}
	combined = strings.TrimSuffix(combined, "\n\n")

	for i, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}
		switch content := msg["content"].(type) {
		case string:
			if content != "" {
				content = combined + "\n\n" + content
			} else {
				content = combined
			}
			msg["content"] = []map[string]any{{"type": "text", "text": content}}
		case []any:
			firstTextIdx := -1
			for j, block := range content {
				b, ok := block.(map[string]any)
				if !ok {
					continue
				}
				if t, _ := b["type"].(string); t == "text" {
					firstTextIdx = j
					break
				}
			}
			if firstTextIdx >= 0 {
				b := content[firstTextIdx].(map[string]any)
				existing, _ := b["text"].(string)
				if existing != "" {
					b["text"] = combined + "\n\n" + existing
				} else {
					b["text"] = combined
				}
				content[firstTextIdx] = b
			} else {
				newContent := make([]any, 0, len(content)+1)
				newContent = append(newContent, map[string]any{"type": "text", "text": combined})
				newContent = append(newContent, content...)
				content = newContent
			}
			msg["content"] = content
		}
		messages[i] = msg
		return
	}
}

func anthropicHeaders() map[string]string {
	return map[string]string{
		"anthropic-dangerous-direct-browser-access": "true",
		"x-app":                         "cli",
		"x-stainless-arch":              stainlessArch(),
		"x-stainless-lang":              "js",
		"x-stainless-os":                stainlessOS(),
		"x-stainless-package-version":   stainlessPkgVersion,
		"x-stainless-retry-count":       "0",
		"x-stainless-runtime":           "node",
		"x-stainless-runtime-version":   stainlessNodeVersion,
		"x-stainless-timeout":           "600",
		"user-agent":                    fmt.Sprintf("claude-cli/%s (external, sdk-cli)", ccVersion),
	}
}

func anthropicQuery() map[string]string {
	return map[string]string{"beta": "true"}
}

func stainlessArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	case "386":
		return "ia32"
	default:
		return runtime.GOARCH
	}
}

func stainlessOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "MacOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return runtime.GOOS
	}
}

var _sessionID string

func sessionID() string {
	if _sessionID != "" {
		return _sessionID
	}
	host, _ := os.Hostname()
	input := fmt.Sprintf("patchbay-%s-%d", host, os.Getpid())
	_sessionID = sha256Hex(input)[:32]
	return _sessionID
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}