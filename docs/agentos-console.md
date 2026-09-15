# AgentOS console delegation

`AGENTOS_CONSOLE_PUBLIC_KEY` contains a base64-encoded RSA public PEM. Empty is
safe by default and disables only this channel. AgentOS holds the matching
private key and signs each knowledge request with a 60-second request-bound
assertion; the user API key is still supplied and must match the mapped external
user. RBAC must be enabled and Redis available. Replay, body, URL, method,
content-type, and key binding are enforced before attaching a live web-user
context. Business ownership and shared-library permissions remain authoritative.

The console bridge is installed before ordinary Auth and is activated only by
`X-AgentOS-Console`. Calls without that header retain the existing JWT/OIDC and
API-key behavior. It never grants an external API key Full Access or changes its
capabilities. It does not reset password-change flags or allow tenant impersonation.

Use AgentOS's `apps/bff/scripts/configure_knowledge_console.py` to configure a
matching pair in separate untracked env files. Restart both services after key
configuration. To disable the bridge, unset the verifier and restart this
backend. No database migration is required. Do not distribute the BFF signer to
browsers, agents, or other integrations.

Validation: `go test ./internal/middleware ./internal/router ./internal/handler`.
The router contract test resolves each allowed bridge route to an existing
handler. The middleware tests exercise signature/request/user binding, existing
restricted keys, ownership, sharing, expiry/replay, membership revocation, and
fail-closed nonce storage.
