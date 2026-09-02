-- The built-in HQZY administrator and workspace are operational bootstrap
-- data. Removing either during a schema rollback could orphan user resources,
-- so this migration is intentionally one-way.
SELECT 1;
