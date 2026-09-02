package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

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
}
