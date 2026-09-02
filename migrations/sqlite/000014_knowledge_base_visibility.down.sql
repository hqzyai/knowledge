DROP INDEX IF EXISTS idx_knowledge_bases_tenant_visibility_creator;
ALTER TABLE knowledge_bases DROP COLUMN visibility;
