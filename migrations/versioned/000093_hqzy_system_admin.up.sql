-- Migration: 000093_hqzy_system_admin
-- Description: Reserve workspace 10000 for AgentOS/WeKnora integration and
-- seed the built-in HQZY platform administrator.
--
-- The plaintext password is deliberately not stored in the repository. Only
-- its bcrypt hash is persisted. This migration is idempotent and also repairs
-- an existing account with the reserved email into the required admin shape.

-- A clean database has no workspace rows. Reserve the first sequence value for
-- the shared system workspace used by external-user provisioning.
INSERT INTO tenants (
    id,
    name,
    description,
    retriever_engines,
    status,
    business,
    created_at,
    updated_at
)
VALUES (
    10000,
    'HQZY System Workspace',
    'Built-in workspace for AgentOS users, shared knowledge bases, and assistants',
    '[]'::jsonb,
    'active',
    'hqzy',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
)
ON CONFLICT (id) DO UPDATE SET
    status = 'active',
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

-- An explicit id does not advance a PostgreSQL sequence. Mark 10000 as used so
-- the next self-service workspace receives 10001 instead of colliding.
SELECT setval(
    pg_get_serial_sequence('tenants', 'id'),
    GREATEST(COALESCE((SELECT MAX(id) FROM tenants), 10000), 10000),
    true
);

DO $$
DECLARE
    admin_user_id VARCHAR(36);
    membership_id BIGINT;
BEGIN
    SELECT id
      INTO admin_user_id
      FROM users
     WHERE LOWER(email) = LOWER('hqzy@admin.com')
     ORDER BY created_at ASC, id ASC
     LIMIT 1;

    IF admin_user_id IS NULL THEN
        admin_user_id := '2ca4d585-f20d-57e1-bac9-87bdbbdb7ea8';
        INSERT INTO users (
            id,
            username,
            email,
            password_hash,
            tenant_id,
            is_active,
            can_access_all_tenants,
            is_system_admin,
            preferences,
            created_at,
            updated_at
        ) VALUES (
            admin_user_id,
            'hqzy_admin',
            'hqzy@admin.com',
            '$2y$12$z650zghm/LmtkLBrcv8sROshtdX9JFNMipLY4b4JkqKn7LQfqFAwO',
            10000,
            TRUE,
            TRUE,
            TRUE,
            '{}'::jsonb,
            CURRENT_TIMESTAMP,
            CURRENT_TIMESTAMP
        );
    ELSE
        -- The email is reserved for this built-in account. Re-applying the
        -- desired state makes upgrades deterministic even if an older manual
        -- bootstrap created it first.
        UPDATE users
           SET password_hash = '$2y$12$z650zghm/LmtkLBrcv8sROshtdX9JFNMipLY4b4JkqKn7LQfqFAwO',
               tenant_id = 10000,
               is_active = TRUE,
               can_access_all_tenants = TRUE,
               is_system_admin = TRUE,
               deleted_at = NULL,
               updated_at = CURRENT_TIMESTAMP
         WHERE id = admin_user_id;
    END IF;

    -- Give the platform admin explicit Owner rights in the shared workspace.
    -- Prefer an active membership; otherwise revive the newest historical row.
    SELECT id
      INTO membership_id
      FROM tenant_members
     WHERE user_id = admin_user_id
       AND tenant_id = 10000
     ORDER BY (deleted_at IS NULL) DESC, id DESC
     LIMIT 1;

    IF membership_id IS NULL THEN
        INSERT INTO tenant_members (
            user_id,
            tenant_id,
            role,
            status,
            joined_at,
            created_at,
            updated_at
        ) VALUES (
            admin_user_id,
            10000,
            'owner',
            'active',
            CURRENT_TIMESTAMP,
            CURRENT_TIMESTAMP,
            CURRENT_TIMESTAMP
        );
    ELSE
        UPDATE tenant_members
           SET role = 'owner',
               status = 'active',
               deleted_at = NULL,
               updated_at = CURRENT_TIMESTAMP
         WHERE id = membership_id;
    END IF;
END $$;
