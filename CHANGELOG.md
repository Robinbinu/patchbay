# Changelog

All notable changes to Patchbay are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [0.1.0-beta.1] - 2026-10-07

First public beta.

### Added

- Local proxy on `127.0.0.1:8787` with OpenAI `/v1/chat/completions`,
  `/v1/responses`, Anthropic `/v1/messages` and a merged `/v1/models`.
- One generated local API key (`pby-…`) for every client; `patchbay key rotate`.
- Model-based routing with provider-prefixed model ids (`grok/grok-4.6`) and
  bare-id resolution.
- Sign-in for ChatGPT Plus/Pro/Team (Codex), Claude Pro/Max, Grok
  (SuperGrok / X Premium+, device code) and OpenRouter, with token refresh and
  expiry tracking. API-key providers for Anthropic and any OpenAI-compatible API.
- Menu-bar / tray app: per-account status and expiry, log in / log out, copy
  endpoint and key, rotate key, stop / start the proxy, status badges.
- **Launch at login** from the menu (macOS LaunchAgent, Windows Run key,
  Linux XDG autostart).
- Links to the project, its author's GitHub and LinkedIn in the menu.
- **Check for updates** from the menu; the app also checks GitHub Releases at
  launch and daily (beta builds are offered betas, stable builds only stable).
- Mono provider icons in the menu, tinted for light and dark menus on macOS.
- `/v1/models` is sorted, includes `created`, and answers Anthropic clients in
  the Anthropic list format with only the models that work on `/v1/messages`.
- macOS universal `Patchbay.app` (in a `.dmg`), Windows tray app with icon and
  version info, Linux and macOS CLI archives.
- `patchbay version`.

### Known limitations

- Request bodies are forwarded as-is; there is no translation between API
  formats yet. Send each model the format of its `surface`.
- macOS builds are ad-hoc signed (not notarized) and Windows builds are not
  code-signed, so first launch needs a manual confirmation.

[0.1.0-beta.1]: https://github.com/Robinbinu/patchbay/releases/tag/v0.1.0-beta.1
