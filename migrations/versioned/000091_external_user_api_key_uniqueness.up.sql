DO $$ BEGIN RAISE NOTICE '[Migration 000084] Enforcing one active API key per external user...'; END $$;

-- API key names remain non-unique for normal operator-created credentials.
-- Only the deterministic external-user/<uuid> namespace is reserved so
-- concurrent provisioning requests across multiple server instances cannot
-- create duplicate active machine credentials for the same external user.
CREATE UNIQUE INDEX IF NOT EXISTS uq_tenant_api_keys_external_user_active
    ON tenant_api_keys (tenant_id, name)
    WHERE revoked_at IS NULL
      AND scope_type = 'tenant'
      AND name LIKE 'external-user/%';

DO $$ BEGIN RAISE NOTICE '[Migration 000084] External-user API key uniqueness ready'; END $$;
