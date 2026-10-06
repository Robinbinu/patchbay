# Contributing to Patchbay

Thanks for helping! Bug reports, provider requests, docs fixes and code are all
welcome.

## Reporting bugs

Open a [bug report](https://github.com/Robinbinu/patchbay/issues/new?template=bug_report.yml)
with your OS, `patchbay version`, what you ran and what happened.

**Never paste tokens or keys.** Redact anything from `~/.patchbay`, `pby-…`
keys, `sk-…` keys and `Authorization` headers.

## Development

Requires Go 1.27+.

```bash
git clone https://github.com/Robinbinu/patchbay
cd patchbay

go build ./cmd/patchbay                # headless proxy + CLI
go build -tags tray ./cmd/patchbay     # with the menu-bar / tray UI

go vet ./... && go vet -tags tray ./...
go test ./...
```

The core proxy and CLI use only the Go standard library; the tray UI
(`fyne.io/systray`) is compiled in only with `-tags tray`. Please keep it that
way: new dependencies need a good reason.

### Layout

| Path | What lives there |
| --- | --- |
| `cmd/patchbay` | CLI entry point, tray UI, icon rendering |
| `internal/autostart` | Launch at login per OS |
| `internal/auth` | OAuth flows (PKCE, device code), token store, refresh |
| `internal/config` | `~/.patchbay/config.json`, provider kinds, local key |
| `internal/providers` | Upstream base URLs, model lists, surfaces |
| `internal/proxy` | HTTP server, routing, forwarding |
| `packaging/`, `scripts/` | macOS `.app` / Windows / archive packaging |

### Adding a provider

1. Add a `Kind…` constant in `internal/config/config.go`.
2. Implement its sign-in in `internal/auth` (see `codex.go`, `xai.go`).
3. Teach `internal/providers/catalog.go` its base URL, surface and model list.
4. Add a favicon under `cmd/patchbay/favicons/`.
5. Document it in the README's Providers table.

## Pull requests

- Branch from `main`; keep each PR focused on one change.
- Run `go vet` and `go test` (CI runs them on macOS, Windows and Linux).
- Add or update tests for behavior changes.
- Explain *why* in the PR description and commit messages.

## Releases

Maintainers push a `vX.Y.Z` (or `vX.Y.Z-beta.N`) tag. The
[release workflow](.github/workflows/release.yml) runs
`scripts/build-release.sh` on macOS and publishes the `.dmg`, Windows zips,
CLI archives and checksums.
