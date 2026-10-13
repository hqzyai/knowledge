package database

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHQZYMigrationVersionsAreUnique(t *testing.T) {
	for _, dialect := range []string{"sqlite", "versioned"} {
		files, err := filepath.Glob(filepath.Join(sqliteRepoRoot(t), "migrations", dialect, "*.up.sql"))
		require.NoError(t, err)
		versions := map[int]string{}
		for _, path := range files {
			version, err := strconv.Atoi(strings.SplitN(filepath.Base(path), "_", 2)[0])
			require.NoError(t, err)
			require.Empty(t, versions[version], "duplicate migration %d: %s / %s", version, versions[version], path)
			versions[version] = path
			_, err = os.Stat(strings.TrimSuffix(path, ".up.sql") + ".down.sql")
			require.NoError(t, err)
		}
	}
}

func TestHQZYUpgradeFromDeployedSQLite19PreservesCustomization(t *testing.T) {
	root := sqliteRepoRoot(t)
	legacy := copySQLiteMigrationsThrough(t, root, 19)
	chdirAndRestore(t, legacy)
	path := filepath.Join(t.TempDir(), "hqzy.db")
	opts := MigrationOptions{SQLiteDBPath: path}
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", opts))
	db := openSQLiteDB(t, path)
	version, dirty := sqliteMigrationState(t, db)
	require.Equal(t, 19, version)
	require.False(t, dirty)
	_, err := db.Exec(`UPDATE users SET password_hash = 'changed-password', username = 'preserved-admin' WHERE email = 'hqzy@admin.com'`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE tenant_api_keys SET key_hash = 'initialized-key', api_key = 'encrypted-sentinel', revoked_at = CURRENT_TIMESTAMP WHERE tenant_id = 10000`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO knowledge_bases (id, tenant_id, name, type, creator_id, visibility, embedding_model_id, summary_model_id) VALUES ('private-sentinel', 10000, 'private', 'document', 'employee-sentinel', 'personal', '', '')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	chdirAndRestore(t, root)
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", opts))
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", opts), "a restart must not replay seeds")
	db = openSQLiteDB(t, path)
	version, dirty = sqliteMigrationState(t, db)
	require.Equal(t, 37, version)
	require.False(t, dirty)
	var password, username, keyHash, token, visibility, creator string
	var revoked bool
	require.NoError(t, db.QueryRow(`SELECT password_hash, username FROM users WHERE email = 'hqzy@admin.com'`).Scan(&password, &username))
	require.Equal(t, "changed-password", password)
	require.Equal(t, "preserved-admin", username)
	require.NoError(t, db.QueryRow(`SELECT key_hash, api_key, revoked_at IS NOT NULL FROM tenant_api_keys WHERE tenant_id = 10000`).Scan(&keyHash, &token, &revoked))
	require.Equal(t, "initialized-key", keyHash)
	require.Equal(t, "encrypted-sentinel", token)
	require.True(t, revoked)
	require.NoError(t, db.QueryRow(`SELECT visibility, creator_id FROM knowledge_bases WHERE id = 'private-sentinel'`).Scan(&visibility, &creator))
	require.Equal(t, "personal", visibility)
	require.Equal(t, "employee-sentinel", creator)
	for _, table := range []string{"knowledge_recall_events", "mcp_endpoints", "browser_devices", "message_artifacts"} {
		require.True(t, sqliteTableExists(t, db, table), table)
	}
	require.True(t, sqliteColumnExists(t, db, "sessions", "fork_bootstrap"))
	require.True(t, sqliteColumnExists(t, db, "knowledge_bases", "profile_config"))
}
