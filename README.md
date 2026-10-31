# MyAgent

A local-first macOS desktop agent. Chat with an agent that uses tools, builds reusable tools and workflows, runs them without chat, waits for approval before risky actions, and improves from real runs.

The implementation follows `DESIGN.md`: Go is the source of truth, every state change is an event, tools and workflows are versioned data, and secrets never enter prompts.

## What is implemented

### Desktop app
- Native macOS shell (`app/main.swift`, WebKit) that starts the Go core and loads the React UI.
- `mise run app` produces `dist/MyAgent.app`; closing the window quits the app and stops the core (schedules/watchers run only while it is open).
- Local HTTP + SSE bridge on loopback, session token/cookie, no cloud dependency.
- Geist Mono throughout, with adjustable text size: sidebar control, Settings → General, or `⌘+` / `⌘−` / `⌘0`.
- Appearance modes in Settings → General: Light, Dark, or System (follows macOS).

### Chat agent
- Streaming markdown chat with collapsible tool cards, approval cards, run links and stop/retry.
- Agent loop with a hard step cap, token budget, context truncation and stop control.
- Meta-tools: `search_tools`, `describe_tool`, `list_workflows`, `get_workflow`, `validate_workflow`, `save_workflow_draft`, `dry_run_workflow`, `run_workflow`, `get_run`, `wait_run`, `list_runs`, `read_run`, `compare_runs`, `get_feedback`, `list_secrets`, `get_environment`, `create_tool_draft`, `write_tool_file`, `read_tool_file`, `list_tool_files`, `run_tool_tests`, `probe_permissions`, `request_tool_approval`, `ask_user`, `remember`, `propose_trigger`, `propose_patch`, `run_eval`.
- “Save as workflow” from a conversation (planner-assisted when a planner is configured, deterministic fallback offline).

### Workflows
- Strict YAML schema (`schema: 1`) with `tool`, `llm`, `agent`, `set`, `branch`, `map`, `parallel`, `approval` and subworkflow steps.
- Static validator with E001–E011 and W001–W004 diagnostics, cycle detection, ancestor-reference checks, tool schema checks, secret-binding checks and self-repair-friendly errors.
- DAG engine with parallel branches, map concurrency, retries/backoff, timeouts, cancel, pause/resume, per-run budget, idempotency keys, output artifacts and resume after crash.
- Live React Flow canvas with node status colors, animated active edges, step inspector (input, output, logs, trace, cost) and approval cards.
- Run history with filters, feedback, rerun and side-by-side comparison.
- Versioning, approval tied to content hash, and rollback.

### Tools
- Built-ins: `fs.list`, `fs.read_text`, `fs.write_text`, `fs.stat`, `fs.delete`, `http.fetch`, `shell.exec`, `workspace.info`, `text.stats`.
- Starlark tools (`go.starlark.net`) with `json`, `http`, `secret`, `log` helpers, execution-step limits and HTTP mocks.
- Python/Node/shell/binary tools through `sandbox-exec` with deny-by-default writes, private-IP blocking and a local HTTP/CONNECT proxy for domain allowlists, header secrets and test mocks.
- Manifest validation, tests (passing + invalid-input), version lifecycle (`draft → tested → active`), content-hash invalidation and approval cards.
- Tool inspector: manifest, schema tables, permissions, secret bindings, code editor, test results, versions and inline run terminal.

### Secrets and safety
- AES-256-GCM with random nonces and `secret_id` as associated data; ciphertext in SQLite, master key outside the DB.
- Master-key providers: `file` (development, 0600) and `keychain` (Security.framework via cgo; opt in with `MYAGENT_KEY_PROVIDER=keychain`).
- API keys for model profiles live in the same secret store.
- Redactor replaces raw, URL-encoded and base64 forms in events, logs, tool results, LLM input/output, artifacts and messages.
- Secret paste detection in chat, secret-name-only visibility for the agent, access log, expiry, overwrite, delete.
- Approval-first writes/external actions, first-run tool approval, version approval, trigger approval.
- Path checks and symlink resolution keep filesystem tools inside the workspace.
- Loopback-only API with session token/SameSite cookie; webhook triggers require a per-trigger token.

### Triggers and runs without chat
- Workflow runs share one start path for manual, chat, schedule, watcher, webhook, CLI and replay; single-tool runs use the same run/event model.
- Cron schedules, folder watchers (FSEvents via fsnotify), local webhook endpoints, CLI (`agentctl`).
- Per-workflow concurrency policies (`queue`, `skip`, `cancel-previous`) and a global run cap. Interrupted runs are paused and resumable.
- Unattended policies (`allow`, `ask`, `deny`), approval expiry, macOS notifications.

### Tuning and eval
- 👍/👎 feedback, eval suites created from real runs with recorded tool I/O.
- Deterministic replay: side-effect tools use recorded outputs and dry-run mocks; LLM steps run live.
- Tuner proposal with diagnosis, diff, accept/reject/rollback and eval run support.

### Models
- Any OpenAI Chat Completions-compatible endpoint: OpenAI, OpenRouter, Ollama, LM Studio, vLLM, Groq, DeepSeek, etc.
- Roles: planner, executor, judge, cheap; default profile for chat.
- Capability probe (streaming, tool calling, JSON mode, latency), retryable error classification, usage/cost accounting and fallback role selection.
- OpenCode Go preset (`https://opencode.ai/zen/go/v1`) with automatic `x-opencode-session` headers. The value is a stable per-conversation (chat) or per-run (workflow) UUID, not a shared static value.

## Requirements

- macOS 13+ (tested on macOS 27 arm64)
- Go 1.27+
- Node 26+ / npm 11+
- Xcode Command Line Tools (`swiftc`, `codesign`) for the native shell

The repository includes `.mise.toml` for Go 1.27.3, Node 26.12.1 and all project tasks.

## Quick start

```bash
mise run deps   # install UI dependencies once
mise run dev    # Go core + Vite hot reload
```

`mise run dev` prints a local URL with a session token. Open it; the UI reads the token, stores it in `localStorage`, and API calls use it. The Vite dev server proxies `/api` to the Go core and exposes a dev-only token endpoint.

Production build:

```bash
mise run build  # UI + bin/agentd + bin/agentctl
mise run run    # serve the production UI on 127.0.0.1:8787
```

Native app bundle:

```bash
mise run app
open dist/MyAgent.app
```

The app bundle contains the Go core, UI and native WebKit shell. It is ad-hoc signed for local use. Distribution requires Developer ID signing and notarization.

### Release

Build a local release zip:

```bash
VERSION=v1.0.0 mise run release
```

Artifacts are written to `dist/release/` (zip + SHA-256). The GitHub workflow `.github/workflows/release.yml` runs the same task on `macos-14` when a `v*` tag is pushed, uploads the artifact, and publishes a GitHub release. Manual `workflow_dispatch` runs upload the artifact without publishing a release.

The release zip is ad-hoc signed, so Gatekeeper may warn on other Macs. Add Developer ID signing/notarization before public distribution.

## First run

- Data: `~/Library/Application Support/MyAgent`
- Workspace: `~/MyAgent Workspace`
- Override with `MYAGENT_DATA_DIR` and `MYAGENT_WORKSPACE`.
- Development (`mise run dev`) uses `./.data` by default.

A fresh install starts with the built-in tools only. Add any OpenAI-compatible model in **Settings → Models** to use chat; local endpoints do not need an API key. Workflows, conversations and workspace files are created by you.

### Useful CLI

```bash
bin/agentctl workflows
bin/agentctl run workspace-digest --input folder=. --input output=digests/digest.md
bin/agentctl runs --limit 10
bin/agentctl status <run-id>
bin/agentctl cancel <run-id>
bin/agentctl tools
```

`agentctl` reads the loopback URL and session token from `<data-dir>/api.json` (0600) and talks to the same API as the UI.

## Architecture

```
Native macOS shell (Swift + WKWebView)
        │ starts core, loads http://127.0.0.1:<random>/?token=…
        ▼
Go core (one process)
  API (stdlib HTTP, token/cookie auth, SSE replay)
  Event bus ──► SQLite events + live UI + trace + eval
  Agent runtime · Workflow engine · Tool registry
  LLM gateway · Starlark/subprocess sandbox · local proxy
  Secret store (AES-GCM + key provider) · cron/watchers · tuner
        │
        ▼
SQLite (WAL, STRICT, UUIDv7, migrations in code)
```

The native shell is intentionally thin. The Go core has no WebKit dependency and can also run headless with `agentd` + `agentctl`.

### Repository layout

```
core packages live under internal/
  agent loop + meta-tools        internal/app
  LLM gateway                    internal/llm
  workflow schema/validator/DAG  internal/workflow
  tools + sandbox + proxy        internal/tools
  SQLite store                   internal/store
  events                         internal/eventbus
  secrets                        internal/secrets
  HTTP/SSE API                   internal/api
  schema validation              internal/schema
app/          Swift native shell + Info.plist
ui/           React + TypeScript + Vite + React Flow
cmd/agentd    server
cmd/agentctl  CLI
scripts/      dev + macOS bundle build
```

## Events

Every persisted state change is an event with a per-stream contiguous `seq`, written to SQLite before broadcast. Ephemeral `*.delta` events are UI-only. Clients reconnect with `GET /api/events?stream=run:<id>&after=<seq>` (or `Last-Event-ID`). The UI loads persisted state through REST and then follows the stream; a gap forces a replay.

Event types include `run.*`, `step.*`, `llm.*`, `tool.call.*`, `approval.*`, `secret.*`, `workflow.*`, `tool.*`, `trigger.*`, `tuning.*`, `eval.*`, `message.created` and `conversation.updated`.

## Security notes

- Tools run with the least privilege declared in the manifest. Built-in HTTP blocks loopback/private/link-local addresses.
- Subprocess tools use `sandbox-exec` with broad system reads (language runtimes need this), but explicit denies for the app data directory, Keychain, `~/.ssh`, `~/.aws`, `~/.config`, `~/.gnupg` and workspace writes only when declared. The workspace and tool directory are re-allowed even in development where they live under the data dir.
- Network allowlists are enforced by a loopback HTTP/CONNECT proxy. Plain HTTP header secrets are injected by the proxy. HTTPS CONNECT is domain-checked but cannot have headers injected without a MITM certificate; prefer env/file injection or the Starlark HTTP helper for header secrets.
- The sandbox denies direct network by default. When a manifest declares domains, raw sockets can still bypass the proxy; treat script tools as code you approve and review their source. Starlark tools are in-process and only get the HTTP helper.
- Secrets are decrypted only at the tool boundary and redacted on the way out. The agent only sees names and status.
- The local API is protected by a per-launch random token and a SameSite cookie. It refuses non-loopback listen addresses.
- The development file key provider is convenient but not the release story; use `MYAGENT_KEY_PROVIDER=keychain` with a signed build for the Keychain provider.

## Tests

```bash
mise run test      # Go tests + UI typecheck
mise run e2e       # Playwright smoke tests (needs: cd ui && npx playwright install chromium)
```

Includes unit tests for schema validation, expression evaluation, workflow validation and DAG execution (retry, approval, resume), AES-GCM secrets and redaction, tool manifests/Starlark execution, proxy allowlists/mocks, LLM SSE streaming/tool-call parsing, and an end-to-end API test that authors a workflow, runs it through human approval, persists appearance settings and tests a freshly created Starlark tool.

## Status and limitations

Implemented in this repository:

- Chat agent + tool loop + approval cards
- Workflow authoring/validation/engine/canvas/history
- Tool authoring/testing/sandbox/approval/versioning
- Secrets, redaction, budgets, model profiles and probes
- Manual/scheduled/watched/webhook/CLI runs
- Feedback, eval replay, tuning proposals and rollback
- Native app bundle and CLI

Not included yet:

- MCP server client/import UI (manifests reserve the `mcp` kind)
- OAuth refresh flows and `op://` 1Password references
- Developer ID signing, notarization, Sparkle auto-update and crash reporting
- Windows/Linux, multi-user/cloud sync and marketplace
- MITM header injection for HTTPS script traffic
- A true menubar/login-item agent (the native app keeps the core alive while running, but does not auto-start at login)
- Full retention/compaction policy for old events and artifacts

The design document remains the reference for the target architecture and roadmap.
