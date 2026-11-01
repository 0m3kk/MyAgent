package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS workspaces (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS model_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  base_url TEXT NOT NULL,
  model TEXT NOT NULL,
  role TEXT NOT NULL,
  params TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(params)),
  capabilities TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(capabilities)),
  secret_id TEXT REFERENCES secrets(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS secrets (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL DEFAULT 'api_key',
  description TEXT NOT NULL DEFAULT '',
  expires_at INTEGER,
  last_used_at INTEGER,
  ciphertext BLOB NOT NULL,
  nonce BLOB NOT NULL,
  key_version INTEGER NOT NULL DEFAULT 1,
  ask_each_run INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS conversations (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  title TEXT NOT NULL DEFAULT 'New chat',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS conversations_workspace ON conversations (workspace_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL CHECK (json_valid(content)),
  model_profile_id TEXT,
  tokens_in INTEGER NOT NULL DEFAULT 0,
  tokens_out INTEGER NOT NULL DEFAULT 0,
  cost_micro INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  UNIQUE (conversation_id, seq)
) STRICT;
CREATE INDEX IF NOT EXISTS messages_conversation ON messages (conversation_id, seq);

CREATE TABLE IF NOT EXISTS tools (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  current_version_id TEXT,
  draft_version_id TEXT,
  folder_id TEXT,
  pinned INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  UNIQUE (workspace_id, name)
) STRICT;

CREATE TABLE IF NOT EXISTS folders (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('tool','workflow')),
  name TEXT NOT NULL,
  parent_id TEXT REFERENCES folders(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS folders_workspace ON folders (workspace_id, kind, name);
CREATE UNIQUE INDEX IF NOT EXISTS folders_unique
  ON folders (workspace_id, kind, COALESCE(parent_id, ''), name);

CREATE TABLE IF NOT EXISTS tool_versions (
  id TEXT PRIMARY KEY,
  tool_id TEXT NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  manifest TEXT NOT NULL,
  code_path TEXT,
  content_hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('draft','tested','approved','active','deprecated')),
  created_by TEXT NOT NULL CHECK (created_by IN ('agent','user')),
  parent_id TEXT,
  approved_at INTEGER,
  created_at INTEGER NOT NULL,
  UNIQUE (tool_id, version)
) STRICT;
CREATE INDEX IF NOT EXISTS tool_versions_tool ON tool_versions (tool_id, version DESC);

CREATE TABLE IF NOT EXISTS tool_secret_bindings (
  tool_id TEXT NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
  logical_name TEXT NOT NULL,
  secret_id TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
  scope_hash TEXT NOT NULL DEFAULT '',
  approved_at INTEGER,
  PRIMARY KEY (tool_id, logical_name)
) STRICT;

CREATE TABLE IF NOT EXISTS workflows (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  current_version_id TEXT,
  folder_id TEXT,
  pinned INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  UNIQUE (workspace_id, name)
) STRICT;

CREATE TABLE IF NOT EXISTS workflow_versions (
  id TEXT PRIMARY KEY,
  workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  yaml TEXT NOT NULL,
  input_schema TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(input_schema)),
  content_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','deprecated')),
  change_note TEXT NOT NULL DEFAULT '',
  parent_id TEXT,
  created_by TEXT NOT NULL CHECK (created_by IN ('agent','user')),
  created_at INTEGER NOT NULL,
  UNIQUE (workflow_id, version)
) STRICT;
CREATE INDEX IF NOT EXISTS workflow_versions_wf ON workflow_versions (workflow_id, version DESC);

CREATE TABLE IF NOT EXISTS triggers (
  id TEXT PRIMARY KEY,
  workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(config)),
  inputs TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(inputs)),
  enabled INTEGER NOT NULL DEFAULT 0,
  unattended TEXT NOT NULL DEFAULT 'ask',
  concurrency TEXT NOT NULL DEFAULT 'queue',
  last_fired_at INTEGER,
  approved_at INTEGER,
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS triggers_workflow ON triggers (workflow_id);

CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  target_kind TEXT NOT NULL CHECK (target_kind IN ('workflow','tool')),
  target_id TEXT,
  target_ver TEXT NOT NULL,
  source TEXT NOT NULL,
  trigger_id TEXT REFERENCES triggers(id) ON DELETE SET NULL,
  conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
  parent_run_id TEXT,
  inputs TEXT NOT NULL CHECK (json_valid(inputs)),
  status TEXT NOT NULL,
  started_at INTEGER,
  finished_at INTEGER,
  tokens_in INTEGER NOT NULL DEFAULT 0,
  tokens_out INTEGER NOT NULL DEFAULT 0,
  cost_micro INTEGER NOT NULL DEFAULT 0,
  error TEXT,
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS runs_target ON runs (target_ver, created_at DESC);
CREATE INDEX IF NOT EXISTS runs_created ON runs (created_at DESC);
CREATE INDEX IF NOT EXISTS runs_status ON runs (status);

CREATE TABLE IF NOT EXISTS step_runs (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  step_id TEXT NOT NULL,
  attempt INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL,
  input_ref TEXT,
  output_ref TEXT,
  error TEXT,
  idem_key TEXT,
  started_at INTEGER,
  finished_at INTEGER,
  UNIQUE (run_id, step_id, attempt)
) STRICT;
CREATE INDEX IF NOT EXISTS step_runs_run ON step_runs (run_id, step_id);

CREATE TABLE IF NOT EXISTS run_bindings (
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  ref TEXT NOT NULL,
  version_id TEXT NOT NULL,
  content_hash TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (run_id, kind, ref)
) STRICT;

CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  stream TEXT NOT NULL,
  seq INTEGER NOT NULL,
  type TEXT NOT NULL,
  ts INTEGER NOT NULL,
  run_id TEXT,
  step_id TEXT,
  schema_ver INTEGER NOT NULL DEFAULT 1,
  payload TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
  UNIQUE (stream, seq)
) STRICT;
CREATE INDEX IF NOT EXISTS events_run ON events (run_id, id);
CREATE INDEX IF NOT EXISTS events_stream ON events (stream, seq);

CREATE TABLE IF NOT EXISTS approvals (
  id TEXT PRIMARY KEY,
  run_id TEXT REFERENCES runs(id) ON DELETE CASCADE,
  step_run_id TEXT,
  conversation_id TEXT REFERENCES conversations(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  request TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(request)),
  status TEXT NOT NULL DEFAULT 'pending',
  decision TEXT,
  comment TEXT,
  expires_at INTEGER,
  decided_at INTEGER,
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS approvals_pending ON approvals (status, created_at);

CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT PRIMARY KEY,
  run_id TEXT,
  step_run_id TEXT,
  path TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  size INTEGER NOT NULL,
  mime TEXT NOT NULL DEFAULT 'application/octet-stream',
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS secret_access_log (
  id TEXT PRIMARY KEY,
  secret_id TEXT NOT NULL,
  run_id TEXT,
  tool_version_id TEXT,
  ts INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS secret_access_log_secret ON secret_access_log (secret_id, ts DESC);

CREATE TABLE IF NOT EXISTS prompt_versions (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  version INTEGER NOT NULL,
  content TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  created_by TEXT NOT NULL DEFAULT 'user',
  created_at INTEGER NOT NULL,
  UNIQUE (name, version)
) STRICT;

CREATE TABLE IF NOT EXISTS workspace_notes (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  note TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS presets (
  id TEXT PRIMARY KEY,
  target_kind TEXT NOT NULL,
  target_id TEXT NOT NULL,
  name TEXT NOT NULL,
  inputs TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(inputs)),
  created_at INTEGER NOT NULL,
  UNIQUE (target_kind, target_id, name)
) STRICT;
`
