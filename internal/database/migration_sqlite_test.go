package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func openMigratedSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoRoot))
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	dbPath := filepath.Join(t.TempDir(), "migration.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLiteMigrationsIncludeAutoTagConfig(t *testing.T) {
	db := openMigratedSQLiteDB(t)

	rows, err := db.Query("PRAGMA table_info(knowledge_bases)")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	found := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		require.NoError(t, rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey))
		if name == "auto_tag_config" {
			found = true
		}
	}
	require.NoError(t, rows.Err())
	require.True(t, found, "SQLite migrations must create knowledge_bases.auto_tag_config")
}

func TestSQLiteMigrationsSeedHQZYSystemAdmin(t *testing.T) {
	db := openMigratedSQLiteDB(t)

	var tenantName, tenantStatus string
	require.NoError(t, db.QueryRow(
		"SELECT name, status FROM tenants WHERE id = ?", 10000,
	).Scan(&tenantName, &tenantStatus))
	require.Equal(t, "HQZY System Workspace", tenantName)
	require.Equal(t, "active", tenantStatus)

	var userID, passwordHash string
	var tenantID int
	var active, allTenants, systemAdmin bool
	require.NoError(t, db.QueryRow(`
		SELECT id, password_hash, tenant_id, is_active,
		       can_access_all_tenants, is_system_admin
		  FROM users
		 WHERE email = ?`, "hqzy@admin.com",
	).Scan(&userID, &passwordHash, &tenantID, &active, &allTenants, &systemAdmin))
	require.Equal(t, "2ca4d585-f20d-57e1-bac9-87bdbbdb7ea8", userID)
	require.Equal(t, 10000, tenantID)
	require.True(t, active)
	require.True(t, allTenants)
	require.True(t, systemAdmin)
	require.NotEmpty(t, passwordHash)
	require.NotEqual(t, "hqzy@admin.com", passwordHash)

	var role, membershipStatus string
	require.NoError(t, db.QueryRow(`
		SELECT role, status
		  FROM tenant_members
		 WHERE user_id = ? AND tenant_id = ? AND deleted_at IS NULL`, userID, 10000,
	).Scan(&role, &membershipStatus))
	require.Equal(t, "owner", role)
	require.Equal(t, "active", membershipStatus)

	var keyID uint64
	var keyName, keyHash, token, scope string
	var fullAccess bool
	var expiresAt sql.NullString
	require.NoError(t, db.QueryRow(`
		SELECT id, name, key_hash, api_key, scope_type, full_access, expires_at
		FROM tenant_api_keys WHERE tenant_id = ?`, 10000,
	).Scan(&keyID, &keyName, &keyHash, &token, &scope, &fullAccess, &expiresAt))
	require.Equal(t, "HQZY Admin Full Access", keyName)
	require.Equal(t, types.HQZYAdminAPIKeyPendingHash, keyHash)
	require.Empty(t, token, "startup must read the configured secret, not embed it in the SQL migration")
	require.Equal(t, "tenant", scope)
	require.True(t, fullAccess)
	require.False(t, expiresAt.Valid)

	// Replaying the seed must preserve an initialized, revoked credential.
	_, err := db.Exec(`UPDATE tenant_api_keys SET key_hash = ?, api_key = ?, revoked_at = CURRENT_TIMESTAMP
		WHERE id = ?`, "initialized-hash", "sk-initialized", keyID)
	require.NoError(t, err)
	migration, err := os.ReadFile("migrations/sqlite/000016_hqzy_admin_api_key.up.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM tenant_api_keys WHERE tenant_id = ?", 10000).Scan(&count))
	require.Equal(t, 1, count)
}
