package database

import (
	"database/sql"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
)

// Use an empty disposable database, separate from the upstream fresh-install test.
func TestHQZYUpgradeFromDeployedPostgres97PreservesCustomization(t *testing.T) {
	dsn := os.Getenv("HQZY_UPGRADE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set HQZY_UPGRADE_TEST_POSTGRES_DSN to an empty disposable database")
	}
	chdirAndRestore(t, sqliteRepoRoot(t))
	m, err := migrate.New("file://migrations/versioned", dsn)
	require.NoError(t, err)
	err = m.Migrate(97)
	require.True(t, err == nil || err == migrate.ErrNoChange, "%v", err)
	sourceErr, dbErr := m.Close()
	require.NoError(t, sourceErr)
	require.NoError(t, dbErr)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`UPDATE users SET password_hash = 'changed-password', username = 'preserved-admin' WHERE email = 'hqzy@admin.com';
		UPDATE tenant_api_keys SET key_hash = 'initialized-key', api_key = 'encrypted-sentinel', revoked_at = CURRENT_TIMESTAMP WHERE tenant_id = 10000;
		INSERT INTO knowledge_bases (id, tenant_id, name, type, creator_id, visibility, embedding_model_id, summary_model_id)
		VALUES ('private-sentinel', 10000, 'private', 'document', 'employee-sentinel', 'personal', '', '');
		INSERT INTO knowledge_recall_events (id,event_id,tenant_id,kb_id,kind,resource_id,actor_id)
		VALUES ('recall-sentinel','event-sentinel',10000,'private-sentinel','document','document-sentinel','employee-sentinel');`)
	require.NoError(t, err)
	require.NoError(t, RunMigrationsWithOptions(dsn, MigrationOptions{}))
	require.NoError(t, RunMigrationsWithOptions(dsn, MigrationOptions{}), "restart must not replay custom seeds")
	var version int
	var dirty bool
	require.NoError(t, db.QueryRow(`SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty))
	require.Equal(t, 117, version)
	require.False(t, dirty)
	var password, username, keyHash, token, visibility, creator, actor string
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
	require.NoError(t, db.QueryRow(`SELECT actor_id FROM knowledge_recall_events WHERE id='recall-sentinel'`).Scan(&actor))
	require.Equal(t, "employee-sentinel", actor)
	for _, table := range []string{"mcp_endpoints", "browser_devices", "message_artifacts"} {
		var exists bool
		require.NoError(t, db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists))
		require.True(t, exists, table)
	}
	for _, field := range [][2]string{{"sessions", "fork_bootstrap"}, {"knowledge_bases", "profile_config"}} {
		var exists bool
		require.NoError(t, db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`, field[0], field[1]).Scan(&exists))
		require.True(t, exists, field)
	}
	requirePostgresIndexValid(t, db)
}
