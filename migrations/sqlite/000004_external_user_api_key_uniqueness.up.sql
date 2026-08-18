-- Preserve duplicate names for ordinary API keys while reserving the
-- deterministic external-user/<uuid> namespace for provisioning idempotency.
CREATE UNIQUE INDEX IF NOT EXISTS uq_tenant_api_keys_external_user_active
    ON tenant_api_keys (tenant_id, name)
    WHERE revoked_at IS NULL
      AND scope_type = 'tenant'
      AND name GLOB 'external-user/*';
