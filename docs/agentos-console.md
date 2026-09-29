# AgentOS console delegation

## Built-in administrator: read-only usage observer

AgentOS's built-in `system_admin` does not need an external employee account.
The BFF signs its stable subject with `role=system_admin` and
`scope=knowledge-analytics:read`. WeKnora binds it to the deterministic UUID
derived from `weknora:agentos-observer:v1:<subject>` and accepts it **only** for
`GET /api/v1/knowledge-analytics`, with no API key or request body. This virtual
observer has no password, employee key or workspace administration role. RSA
request binding, the 60-second lifetime, live workspace status and Redis replay
protection all remain mandatory. Other methods and all employee console routes
are denied; ordinary users and keys cannot call the observer endpoint.

Analytics covers workspace 10000. Query parameters are
`view=libraries|documents|wikis|users|sources`, `days=7|30|90|all`, `kb_id`,
optional `resource_type=document|wiki` and `resource_id`, `q`, `page`, and
`page_size` (max 100). The response includes paginated items, summary, collection
start, selected library/resource and observer identity. Source-document counts
are deduplicated Wiki `source_refs` relationships, not retrieval events.

Migration 97 (SQLite 19) starts durable recall collection; earlier history is
unavailable. One successful direct hybrid search, final pipeline result, final
`knowledge_search` output or Wiki HTTP search / permission-filtered `wiki_search` output is one
retrieval. Each returned library/document/Wiki is counted once per retrieval.
Page browsing, intermediate candidates, and Wiki generation are excluded.
Employee API-key attribution comes from its persisted `external-user/<UUID>`
binding, regardless of caller-supplied external identity headers. Recording is
bounded to three seconds; failures are logged and do not break retrieval.
An unavailable statistics store returns an error instead of zero counts.

## Employee console

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
