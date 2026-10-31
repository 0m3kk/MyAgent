<p align="center">
  <img src="docs/icon.png" width="112" alt="MyAgent icon" />
</p>

<h1 align="center">MyAgent</h1>

<p align="center">
  <strong>A local-first macOS agent that gets real work done.</strong><br />
  Chat, use tools, build reusable workflows, approve risky actions, and improve from every run.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/macOS-13%2B-111111?logo=apple&logoColor=white&style=flat-square" alt="macOS 13+" />
  <img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white&style=flat-square" alt="Go 1.27" />
  <img src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=111111&style=flat-square" alt="React 19" />
  <img src="https://img.shields.io/badge/SQLite-local-003B57?logo=sqlite&logoColor=white&style=flat-square" alt="SQLite" />
  <img src="https://img.shields.io/badge/LLM-OpenAI--compatible-7C6CFF?style=flat-square" alt="OpenAI-compatible" />
</p>

---

MyAgent is a native macOS desktop app with a Go core. Everything runs on your Mac: the database, tools, secrets, workflow engine, traces and model gateway. There is no cloud account, no background SaaS, and no hidden execution path.

- **Approval first.** Writes, external actions and first runs wait for you.
- **Reusable by design.** A successful chat becomes a versioned workflow; missing capabilities become versioned tools.
- **Observable.** Every state change is a persisted event. Runs, traces, cost and eval replay share one source of truth.
- **Bring your own model.** Any OpenAI Chat Completions-compatible endpoint works, including local runtimes.

## Quick start

Requirements: macOS 13+, [mise](https://mise.jdx.dev/), Xcode Command Line Tools.

```bash
mise install       # Go 1.27.3 + Node 26.12.1 from .mise.toml
mise run deps      # install UI dependencies
mise run dev       # Go core + Vite with HMR
```

The dev command prints a loopback URL with a session token. The UI stores it locally; the Vite server proxies `/api` to the Go core.

### Native app

```bash
mise run app
open dist/MyAgent.app
```

Closing the window quits the app and stops the core. Scheduled and watched runs only execute while the app is open.

### First run

- Data: `~/Library/Application Support/MyAgent`
- Workspace: `~/MyAgent Workspace`
- Override with `MYAGENT_DATA_DIR` and `MYAGENT_WORKSPACE`.

A fresh install starts with built-in tools only. Add a model in **Settings → Models** to use chat. Workflows, conversations and workspace files are created by you.

## What's inside

| Area | Capabilities |
| --- | --- |
| **Chat agent** | Streaming markdown, collapsible tool cards, approval cards, stop, run links, “Save as workflow”, meta-tools and budget/step limits. |
| **Workflows** | Strict YAML schema, structured validator, DAG engine, parallel/map/branch/agent steps, retries, timeouts, pause/resume, crash resume, budget limits. |
| **Canvas** | Live React Flow graph, status colors, animated active edges, per-step inspector for input, output, logs, trace and cost. |
| **Tools** | Built-ins, Starlark, Python/Node/shell/binary sandbox, manifests, tests, version lifecycle, code inspector and inline terminal. |
| **Safety** | AES-256-GCM secret store, master key outside SQLite, redaction everywhere, path checks, loopback-only API, per-trigger tokens. |
| **Runs** | History, filters, feedback, rerun, side-by-side comparison, live events and resumable interrupted runs. |
| **Tuning** | Eval suites from real runs, deterministic replay, tuner diagnosis, diff review, accept/reject/rollback. |
| **Triggers** | Cron, folder watchers, local webhooks, CLI, unattended policies and concurrency policies. |
| **Desktop** | Native Swift/WebKit shell, Geist Mono UI, adjustable text size, Light/Dark/System appearance. |

## Models

Any OpenAI-compatible endpoint works: OpenAI, OpenRouter, Ollama, LM Studio, vLLM, Groq, DeepSeek and others.

| Preset | Base URL |
| --- | --- |
| OpenAI | `https://api.openai.com/v1` |
| OpenRouter | `https://openrouter.ai/api/v1` |
| OpenCode Go | `https://opencode.ai/zen/go/v1` |
| Ollama | `http://127.0.0.1:11434/v1` |
| LM Studio | `http://127.0.0.1:1234/v1` |
| DeepSeek | `https://api.deepseek.com/v1` |
| Groq | `https://api.groq.com/openai/v1` |

**OpenCode Go** automatically sends `x-opencode-session` with a stable per-conversation UUID (chat) or per-run UUID (workflow). The value is never a shared static session. For other gateways, set a custom **Session header** in the model profile.

Model roles: `planner`, `executor`, `judge`, `cheap`. Probe a profile to verify streaming and tool calling before assigning it to workflow authoring.

## Tasks

All project commands live in `.mise.toml`:

| Task | What it does |
| --- | --- |
| `mise run deps` | Install UI dependencies |
| `mise run dev` | Go core + Vite dev server |
| `mise run ui` | Build the production UI |
| `mise run api` | Build `bin/agentd` and `bin/agentctl` |
| `mise run build` | Build UI and Go binaries |
| `mise run run` | Serve the production UI on `127.0.0.1:8787` |
| `mise run app` | Build `dist/MyAgent.app` |
| `mise run release` | Build and package a macOS release zip |
| `mise run test` | Go tests + UI typecheck |
| `mise run e2e` | Playwright smoke tests |
| `mise run clean` | Remove build output and local data |

## CLI

```bash
bin/agentctl workflows
bin/agentctl run workspace-digest --input folder=. --input output=digests/digest.md
bin/agentctl runs --limit 10
bin/agentctl status <run-id>
bin/agentctl cancel <run-id>
bin/agentctl tools
```

`agentctl` reads the loopback URL and session token from `<data-dir>/api.json` (mode `0600`) and talks to the same local API as the UI.

## Architecture

```text
Native macOS shell (Swift + WKWebView)
        │ starts core, loads http://127.0.0.1:<random>/?token=…
        ▼
Go core
  HTTP API + SSE replay · event bus · SQLite projections
  Agent runtime · workflow engine · tool registry
  LLM gateway · Starlark/subprocess sandbox · local proxy
  Secret store · cron/watchers · tuner
        │
        ▼
SQLite (WAL, STRICT, UUIDv7, migrations in code)
```

The native shell is intentionally thin. The Go core has no WebKit dependency and can run headless with `agentd` + `agentctl`.

### Repository map

```text
internal/app        agent loop, meta-tools, chat, runs, triggers
internal/llm        OpenAI-compatible gateway, probe, cost
internal/workflow   schema, validator, expressions, DAG engine
internal/tools      registry, builtins, Starlark/script sandbox, proxy
internal/store      SQLite schema and queries
internal/eventbus   persisted events + live fan-out
internal/secrets    AES-GCM store, key providers, redaction
internal/api        HTTP/SSE handlers
internal/schema     JSON Schema validation
app/                Swift native shell + Info.plist + icon
ui/                 React + TypeScript + Vite + React Flow
cmd/agentd          local core server
cmd/agentctl        CLI
```

## Security

- **Secrets** are AES-256-GCM encrypted in SQLite. The master key is outside the DB: a 0600 dev file or macOS Keychain (`MYAGENT_KEY_PROVIDER=keychain`).
- **Redaction** covers raw, URL-encoded and base64 forms in events, logs, tool results, messages, artifacts and model I/O.
- **Approvals** are required for writes, external actions, new tools, version changes and trigger creation. Approval is tied to the content hash.
- **Filesystem tools** resolve symlinks and stay inside the workspace.
- **Subprocess tools** run under `sandbox-exec`, deny the app data directory and common credential stores, and only write where declared.
- **Network tools** use a loopback HTTP/CONNECT proxy with domain allowlists, private-IP blocking and header-secret injection for plain HTTP.
- **The API** listens on loopback only, requires a per-launch token or SameSite cookie, and webhook triggers require their own token.

## Release

```bash
VERSION=v1.0.0 mise run release
```

Artifacts go to `dist/release/` as a zip plus SHA-256. The GitHub workflow `.github/workflows/release.yml` builds on `macos-14` for `v*` tags, uploads the artifact and publishes a GitHub release. Manual `workflow_dispatch` runs upload the artifact without publishing.

The local release is ad-hoc signed. Public distribution still needs Developer ID signing and notarization.

## Tests

```bash
mise run test      # Go tests + UI typecheck
mise run e2e       # Playwright smoke tests
```

Coverage includes schema/expression validation, workflow validation and DAG execution (retry, approval, resume), AES-GCM secrets and redaction, tool manifests/Starlark execution, proxy allowlists/mocks, LLM SSE streaming and tool calls, and an end-to-end API test that authors a workflow, runs it through human approval, persists appearance settings and tests a freshly created tool.

## Status and limitations

### Implemented

Chat agent and tool loop · workflow authoring, engine, canvas and history · tool authoring, testing, sandbox and versioning · secrets and redaction · models and probes · manual, scheduled, watched, webhook and CLI runs · eval replay and tuning · native app and CLI.

### Not included yet

- MCP server client and import UI (the `mcp` tool kind is reserved)
- OAuth refresh flows and `op://` 1Password references
- Developer ID signing, notarization, Sparkle auto-update and crash reporting
- Windows/Linux, multi-user/cloud sync and marketplace
- MITM header injection for HTTPS script traffic
- Menubar/login-item background agent (runs only while the app is open)
- Automatic retention/compaction for old events and artifacts

The full design reference and roadmap live in [`DESIGN.md`](DESIGN.md).
