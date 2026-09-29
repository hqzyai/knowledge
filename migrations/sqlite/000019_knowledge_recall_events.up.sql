CREATE TABLE IF NOT EXISTS knowledge_recall_events (
  id TEXT PRIMARY KEY, event_id TEXT NOT NULL, tenant_id INTEGER NOT NULL,
  kb_id TEXT NOT NULL, kind TEXT NOT NULL, resource_id TEXT NOT NULL,
  actor_id TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (event_id, kb_id, kind, resource_id)
);
CREATE INDEX IF NOT EXISTS idx_recall_scope_time ON knowledge_recall_events (tenant_id, kb_id, created_at);
CREATE INDEX IF NOT EXISTS idx_recall_resource_time ON knowledge_recall_events (tenant_id, kind, resource_id, created_at);
CREATE TABLE IF NOT EXISTS knowledge_recall_collection (id INTEGER PRIMARY KEY, started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT INTO knowledge_recall_collection (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
