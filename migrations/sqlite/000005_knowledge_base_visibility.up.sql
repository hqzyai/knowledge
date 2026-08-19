ALTER TABLE knowledge_bases
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'personal';

UPDATE knowledge_bases
SET visibility = 'workspace';

UPDATE knowledge_bases
SET visibility = 'personal'
WHERE description = 'Hermes 自动同步的用户私有对话知识库';

CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_visibility_creator
    ON knowledge_bases (tenant_id, visibility, creator_id)
    WHERE deleted_at IS NULL AND is_temporary = 0;
