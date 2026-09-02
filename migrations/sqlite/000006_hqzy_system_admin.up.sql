-- SQLite mirror of PostgreSQL migration 000086_hqzy_system_admin.

-- SQLite's consolidated baseline predates platform administrators.
ALTER TABLE users ADD COLUMN is_system_admin BOOLEAN NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_users_is_system_admin ON users(is_system_admin);

INSERT OR IGNORE INTO tenants (
    id,
    name,
    description,
    retriever_engines,
    status,
    business,
    created_at,
    updated_at
) VALUES (
    10000,
    'HQZY System Workspace',
    'Built-in workspace for AgentOS users, shared knowledge bases, and assistants',
    '[]',
    'active',
    'hqzy',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
);

UPDATE tenants
   SET status = 'active',
       deleted_at = NULL,
       updated_at = CURRENT_TIMESTAMP
 WHERE id = 10000;

-- Keep AUTOINCREMENT ahead of the reserved workspace id.
UPDATE sqlite_sequence
   SET seq = MAX(seq, (SELECT MAX(id) FROM tenants))
 WHERE name = 'tenants';

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
)
SELECT
    '2ca4d585-f20d-57e1-bac9-87bdbbdb7ea8',
    'hqzy_admin',
    'hqzy@admin.com',
    '$2y$12$z650zghm/LmtkLBrcv8sROshtdX9JFNMipLY4b4JkqKn7LQfqFAwO',
    10000,
    1,
    1,
    1,
    '{}',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
WHERE NOT EXISTS (
    SELECT 1 FROM users WHERE LOWER(email) = LOWER('hqzy@admin.com')
);

UPDATE users
   SET password_hash = '$2y$12$z650zghm/LmtkLBrcv8sROshtdX9JFNMipLY4b4JkqKn7LQfqFAwO',
       tenant_id = 10000,
       is_active = 1,
       can_access_all_tenants = 1,
       is_system_admin = 1,
       deleted_at = NULL,
       updated_at = CURRENT_TIMESTAMP
 WHERE LOWER(email) = LOWER('hqzy@admin.com');

INSERT OR IGNORE INTO tenant_members (
    user_id,
    tenant_id,
    role,
    status,
    joined_at,
    created_at,
    updated_at
)
SELECT
    id,
    10000,
    'owner',
    'active',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
FROM users
WHERE LOWER(email) = LOWER('hqzy@admin.com');

UPDATE tenant_members
   SET role = 'owner',
       status = 'active',
       deleted_at = NULL,
       updated_at = CURRENT_TIMESTAMP
 WHERE tenant_id = 10000
   AND user_id IN (
       SELECT id FROM users WHERE LOWER(email) = LOWER('hqzy@admin.com')
   );
