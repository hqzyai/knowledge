DROP INDEX IF EXISTS idx_knowledge_bases_tenant_visibility_creator;
ALTER TABLE knowledge_bases DROP CONSTRAINT IF EXISTS chk_knowledge_bases_visibility;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS visibility;
