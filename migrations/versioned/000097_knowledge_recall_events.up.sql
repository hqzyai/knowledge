CREATE TABLE IF NOT EXISTS knowledge_recall_events (
  id VARCHAR(36) PRIMARY KEY,
  event_id VARCHAR(36) NOT NULL,
  tenant_id BIGINT NOT NULL,
  kb_id VARCHAR(36) NOT NULL,
  kind VARCHAR(16) NOT NULL,
  resource_id VARCHAR(128) NOT NULL,
  actor_id VARCHAR(256) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (event_id, kb_id, kind, resource_id)
);
CREATE INDEX IF NOT EXISTS idx_recall_scope_time ON knowledge_recall_events (tenant_id, kb_id, created_at);
CREATE INDEX IF NOT EXISTS idx_recall_resource_time ON knowledge_recall_events (tenant_id, kind, resource_id, created_at);
CREATE TABLE IF NOT EXISTS knowledge_recall_collection (
  id INTEGER PRIMARY KEY,
  started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO knowledge_recall_collection (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
