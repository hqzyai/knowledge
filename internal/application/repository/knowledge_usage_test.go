package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func usageFixture(t *testing.T) (*KnowledgeUsageRepository, context.Context) {
	t.Helper()
	var driver gorm.Dialector = sqlite.Open(":memory:")
	postgresDSN := os.Getenv("KNOWLEDGE_USAGE_TEST_POSTGRES_DSN")
	if postgresDSN != "" {
		driver = postgres.Open(postgresDSN)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	migration := "../../../migrations/sqlite/000019_knowledge_recall_events.up.sql"
	if postgresDSN != "" {
		migration = "../../../migrations/versioned/000097_knowledge_recall_events.up.sql"
	}
	ddl, err := os.ReadFile(migration)
	require.NoError(t, err)
	// PostgreSQL verification uses connection-local temporary tables. Closing
	// this fixture drops them without touching the deployed knowledge data.
	schema := string(ddl)
	if postgresDSN != "" {
		schema = strings.ReplaceAll(schema, "CREATE TABLE IF NOT EXISTS", "CREATE TEMP TABLE IF NOT EXISTS")
	}
	require.NoError(t, db.Exec(schema).Error)
	for _, stmt := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER, name TEXT, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME)`,
		`CREATE TABLE wiki_pages (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, source_refs TEXT, deleted_at DATETIME)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT)`,
		`CREATE TABLE tenant_api_keys (id INTEGER PRIMARY KEY, name TEXT)`,
		`INSERT INTO knowledge_bases VALUES ('kb',10000,'Library',NULL),('zero',10000,'Unused',NULL),('foreign',20000,'Private',NULL)`,
		`INSERT INTO knowledges VALUES ('doc',10000,'kb','Document',NULL),('foreign-doc',20000,'foreign','Hidden',NULL)`,
		`INSERT INTO wiki_pages VALUES ('wiki',10000,'kb','Wiki','["doc|Document","doc|Duplicate","missing|Old"]',NULL)`,
		`INSERT INTO users VALUES ('alice','Alice'),('bob','Bob')`,
		`INSERT INTO tenant_api_keys VALUES (7,'external-user/alice')`,
	} {
		if postgresDSN != "" {
			stmt = strings.ReplaceAll(strings.ReplaceAll(stmt, "CREATE TABLE", "CREATE TEMP TABLE"), "DATETIME", "TIMESTAMPTZ")
		}
		require.NoError(t, db.Exec(stmt).Error)
	}
	return NewKnowledgeUsageRepository(db), types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "alice"})
}

func TestKnowledgeUsageDeduplicatesAndScopes(t *testing.T) {
	r, ctx := usageFixture(t)
	hits := []types.RecallHit{{KnowledgeBaseID: "kb", ResourceID: "doc", Kind: "document"}, {KnowledgeBaseID: "kb", ResourceID: "doc", Kind: "document"}, {KnowledgeBaseID: "kb", ResourceID: "wiki", Kind: "wiki"}}
	require.NoError(t, r.record(ctx, hits))
	bob := types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "bob"})
	require.NoError(t, r.record(bob, hits[:1]))
	require.NoError(t, r.record(ctx, []types.RecallHit{{ResourceID: "foreign-doc", Kind: "document"}}))
	q := KnowledgeUsageQuery{View: "libraries", Page: 1, PageSize: 20, Days: 30}
	result, err := r.Read(ctx, 10000, q)
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Total)
	require.EqualValues(t, 2, result.Summary.Recalls)
	require.EqualValues(t, 2, result.Summary.DocumentRecalls)
	require.EqualValues(t, 1, result.Summary.WikiRecalls)
	require.EqualValues(t, 2, result.Summary.Users)
	require.Equal(t, "kb", result.Items[0].ID)
	require.EqualValues(t, 0, result.Items[1].Recalls)
	require.NotNil(t, result.Items[0].LastRecall)
	q.KBID, q.View = "kb", "wikis"
	result, err = r.Read(ctx, 10000, q)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Items[0].Recalls)
	require.Equal(t, 2, result.Items[0].SourceCount)
	q.ResourceID, q.ResourceType, q.View = "wiki", "wiki", "users"
	result, err = r.Read(ctx, 10000, q)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "Alice", result.Items[0].Name)
	q.View = "sources"
	result, err = r.Read(ctx, 10000, q)
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.Equal(t, "Document", result.Items[0].Name)
	require.True(t, result.Items[1].Deleted)
	q.KBID = "foreign"
	_, err = r.Read(ctx, 10000, q)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestKnowledgeUsageAttributionPeriodAndPagination(t *testing.T) {
	r, ctx := usageFixture(t)
	// A caller's spoofed external identity must not override the persisted key binding.
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPIExternalUser, ID: "10000:mallory"})
	ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 7})
	require.NoError(t, r.record(ctx, []types.RecallHit{{ResourceID: "doc", Kind: "document"}}))
	q := KnowledgeUsageQuery{View: "users", KBID: "kb", Page: 1, PageSize: 1, Days: 7}
	result, err := r.Read(ctx, 10000, q)
	require.NoError(t, err)
	require.Equal(t, "alice", result.Items[0].ID)
	q.Search = "nobody"
	result, err = r.Read(ctx, 10000, q)
	require.NoError(t, err)
	require.Zero(t, result.Total)
	require.Empty(t, result.Items)
	require.NoError(t, r.db.Model(&recallEvent{}).Where("1 = 1").Update("created_at", time.Now().AddDate(0, 0, -8)).Error)
	q.Search = ""
	result, err = r.Read(ctx, 10000, q)
	require.NoError(t, err)
	require.Zero(t, result.Total)
	q.Days = 0
	result, err = r.Read(ctx, 10000, q)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Total)
	q.Page = 2
	result, err = r.Read(ctx, 10000, q)
	require.NoError(t, err)
	require.Empty(t, result.Items)
	require.EqualValues(t, 1, result.Total)
}
