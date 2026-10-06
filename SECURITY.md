# Security Policy

Patchbay holds OAuth tokens and API keys for your model providers, so we take
security reports seriously.

## Supported versions

Only the latest release (including betas) receives fixes.

## Reporting a vulnerability

Please **do not open a public issue**. Report privately through
[GitHub security advisories](https://github.com/Robinbinu/patchbay/security/advisories/new).

Include what you found, how to reproduce it, and the impact. We aim to reply
within a few days and will credit you in the release notes unless you prefer
otherwise.

## Scope

In scope: anything that could expose `~/.patchbay` credentials or the local
API key, let another local user or a web page reach the proxy without the key,
or send requests or tokens to an unintended host.

## Hardening notes

- The proxy listens on `127.0.0.1` by default. Binding it to a public interface
  exposes your subscriptions to anyone holding the local key.
- `~/.patchbay` is created `0700` and its files `0600`.
- Rotate the local key with `patchbay key rotate` if it leaks.
