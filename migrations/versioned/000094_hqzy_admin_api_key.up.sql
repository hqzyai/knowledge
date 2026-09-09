-- Seed a single workspace credential for the built-in administrator. The
-- application installs WEKNORA_BOOTSTRAP_ADMIN_API_KEY through the AES model
-- hook at startup; no shared secret is embedded in SQL.
INSERT INTO tenant_api_keys (
    tenant_id, scope_type, name, key_hash, api_key, full_access,
    knowledge_base_ids, capabilities
)
SELECT
    10000, 'tenant', 'HQZY Admin Full Access',
    'bootstrap-hqzy-admin-pending', '', TRUE, '[]'::jsonb, '[]'::jsonb
WHERE EXISTS (
    SELECT 1 FROM users u
    JOIN tenant_members m ON m.user_id = u.id AND m.tenant_id = 10000
    WHERE LOWER(u.email) = 'hqzy@admin.com'
      AND u.tenant_id = 10000 AND u.is_active = TRUE AND u.is_system_admin = TRUE
      AND u.deleted_at IS NULL AND m.deleted_at IS NULL
      AND m.role = 'owner' AND m.status = 'active'
)
AND NOT EXISTS (
    SELECT 1 FROM tenant_api_keys
    WHERE tenant_id = 10000 AND name = 'HQZY Admin Full Access'
)
ON CONFLICT (key_hash) DO NOTHING;
