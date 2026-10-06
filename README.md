# Patchbay

One local endpoint for every model you have access to.

Patchbay is a small, OS-agnostic proxy written in Go. You sign in to your
providers once, and Patchbay exposes them all behind a single local URL with
one local API key — speaking both the **OpenAI** and **Anthropic** wire
formats — so any coding tool that talks to either API can reach any of your
models. It tracks each sign-in and tells you, in a menu-bar icon, when a login
is about to expire.

> A patch bay is the panel in a studio that routes many sources to many
> destinations through one board. Same idea, for models.

## What it does

- **Unified endpoints**
  - `POST /v1/chat/completions` — OpenAI chat-completions
  - `POST /v1/responses` — OpenAI/Codex Responses
  - `POST /v1/messages` — Anthropic Messages
  - `GET  /v1/models` — every model from every signed-in provider, in one list
- **One local API key.** Clients authenticate to Patchbay with a generated
  `pby-…` key (as a `Bearer` token or `x-api-key`). Your upstream provider
  credentials never leave the machine.
- **Model-based routing.** Patchbay builds a model → provider index from each
  provider's own model list and routes every request to the owner of the
  requested `model`. `/v1/models` advertises **provider-prefixed** ids
  (`codex/gpt-5.6-luna`, `grok/grok-4.6`, `openrouter/openai/gpt-5.5`); send a
  prefixed id to pick a provider explicitly, or a bare id to let Patchbay
  resolve it. The upstream always sees its own bare model id.
- **Login status & expiry.** `patchbay status` and the menu bar show each
  account, its plan, and when the access token and the overall login expire,
  with a one-click **Log in again** when a login lapses.

## Providers

| Kind | Auth | Surface |
| --- | --- | --- |
| `codex-oauth` | ChatGPT Plus/Pro/Team plan, OAuth (PKCE) | Responses |
| `anthropic-oauth` | Claude Pro/Max, OAuth (PKCE) | Messages |
| `xai-oauth` | Grok (SuperGrok / X Premium+), OAuth (device-code) | Responses |
| `openrouter-oauth` | OpenRouter, OAuth (PKCE) → durable key | Chat |
| `anthropic-key` | Anthropic Console API key | Messages |
| `openai-key` | OpenAI-compatible API key (OpenAI, OpenRouter, DeepSeek, …) | Chat/Responses |

Seeded providers (`codex`, `claude`, `grok`, `openrouter`) are ready to
`login`; add more with `patchbay provider add`.

The OAuth flows (authorize URL, PKCE, loopback callback, token exchange,
refresh, identity, expiry) follow the approach used by
[oh-my-pi](https://www.npmjs.com/package/@oh-my-pi/pi-ai)'s auth rules. More
providers can be added behind the same seams.

## Install

Requires Go 1.27+.

```bash
# Core proxy + CLI (standard library only)
go build -o patchbay ./cmd/patchbay

# With the macOS/Windows/Linux menu-bar UI
go build -tags tray -o patchbay ./cmd/patchbay
```

## Use

```bash
# 1. Sign in to the providers you want
patchbay login codex      # opens the browser (ChatGPT plan)
patchbay login claude     # opens the browser (Claude Pro/Max)
patchbay login grok       # device code: open the URL, enter the code
patchbay login openrouter # opens the browser; mints a durable key

# 2. (optional) Add an API-key provider
patchbay provider add openai openai-key
patchbay provider key openai sk-...
patchbay provider add anthropic anthropic-key
patchbay provider key anthropic sk-ant-...

# 3. Check sign-in / expiry
patchbay status

# 4. Run it
patchbay serve
```

Point any tool at it:

```bash
# OpenAI-compatible client
export OPENAI_BASE_URL=http://127.0.0.1:8787/v1
export OPENAI_API_KEY=$(patchbay key)

# Anthropic-compatible client
export ANTHROPIC_BASE_URL=http://127.0.0.1:8787
export ANTHROPIC_API_KEY=$(patchbay key)
```

`patchbay key rotate` issues a new local key; `patchbay logout <id>` forgets a
provider's tokens.

## Where state lives

Everything is under `~/.patchbay`, created `0700`:

- `config.json` — listen address, local API key, provider list (`0600`)
- `credentials.json` — OAuth tokens and expiry per provider (`0600`)

Nothing is sent anywhere except the provider you are calling.

## Status and limits

This is an early build. Request bodies are forwarded to each provider's native
surface as-is; cross-format translation (e.g. an OpenAI chat-completions body
to an Anthropic Messages body) is not done yet — send a provider the format its
surface expects, or use `/v1/models` to see which surface a model is on.

## License

MIT. See [LICENSE](LICENSE).
