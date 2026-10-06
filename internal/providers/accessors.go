package providers

// Small accessors so the proxy package can read upstream wire constants without
// duplicating them.

// CodexClientVersion is the Codex client version the backend gates models on.
func CodexClientVersion() string { return codexClientVersion }

// CodexUserAgent is Patchbay's honest Codex user-agent.
func CodexUserAgent() string { return codexUserAgent }

// AnthropicVersion is the Anthropic API version header value.
func AnthropicVersion() string { return anthropicVersion }

// AnthropicOAuthBeta is the beta flag an Anthropic OAuth token requires.
func AnthropicOAuthBeta() string { return anthropicOAuthBeta }
