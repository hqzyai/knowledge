DO $$ BEGIN RAISE NOTICE '[Migration 000085] Adding workspace-local knowledge base visibility...'; END $$;

-- New knowledge bases are private by default. Existing rows are backfilled to
-- workspace visibility first so this migration does not silently change the
-- behaviour of ordinary legacy workspaces.
ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS visibility VARCHAR(16) NOT NULL DEFAULT 'personal';

UPDATE knowledge_bases
SET visibility = 'workspace';

-- Hermes conversation knowledge bases were always intended to be private.
-- They carry this stable description from the sync service, so repair those
-- rows while preserving legacy visibility for every other knowledge base.
UPDATE knowledge_bases
SET visibility = 'personal'
WHERE description = 'Hermes 自动同步的用户私有对话知识库';

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS chk_knowledge_bases_visibility;
ALTER TABLE knowledge_bases
    ADD CONSTRAINT chk_knowledge_bases_visibility
    CHECK (visibility IN ('personal', 'workspace'));

CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_visibility_creator
    ON knowledge_bases (tenant_id, visibility, creator_id)
    WHERE deleted_at IS NULL AND is_temporary = FALSE;

DO $$ BEGIN RAISE NOTICE '[Migration 000085] Knowledge base visibility ready'; END $$;
