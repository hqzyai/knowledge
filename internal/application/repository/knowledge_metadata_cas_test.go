package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCompareAndSwapKnowledgeMetadataAndStaleSaveProtection(t *testing.T) {
	db := setupKnowledgeTestDB(t)
	require.NoError(t, db.Exec(`ALTER TABLE knowledges ADD COLUMN custom_metadata TEXT NOT NULL DEFAULT '{}'`).Error)
	repo := NewKnowledgeRepository(db).(*knowledgeRepository)
	ctx := context.Background()
	row := &types.Knowledge{
		ID:              uuid.NewString(),
		TenantID:        71,
		KnowledgeBaseID: uuid.NewString(),
		Type:            types.KnowledgeTypeManual,
		Title:           "daily",
		Source:          types.KnowledgeTypeManual,
		ParseStatus:     types.ParseStatusCompleted,
		Metadata:        types.JSON(`{"content":"one","format":"markdown","status":"publish","version":1}`),
	}
	require.NoError(t, repo.CreateKnowledge(ctx, row))

	stale, err := repo.GetKnowledgeByID(ctx, row.TenantID, row.ID)
	require.NoError(t, err)
	expected := append(types.JSON(nil), stale.Metadata...)
	replacement := types.JSON(`{"content":"one\n\ntwo","format":"markdown","status":"publish","version":2}`)

	updated, err := repo.CompareAndSwapKnowledgeMetadata(
		ctx, row.TenantID, row.ID, expected, replacement,
		map[string]interface{}{"parse_status": types.ParseStatusPending},
	)
	require.NoError(t, err)
	require.True(t, updated)

	updated, err = repo.CompareAndSwapKnowledgeMetadata(
		ctx, row.TenantID, row.ID, expected, types.JSON(`{"version":3}`), nil)
	require.NoError(t, err)
	require.False(t, updated, "the same stale revision must not win twice")

	// A worker that loaded v1 before the append may still save unrelated state.
	// UpdateKnowledge must persist that state without restoring stale metadata.
	stale.Title = "worker-updated-title"
	stale.Metadata = types.JSON(`{"content":"stale","version":1}`)
	require.NoError(t, repo.UpdateKnowledge(ctx, stale))

	current, err := repo.GetKnowledgeByID(ctx, row.TenantID, row.ID)
	require.NoError(t, err)
	require.Equal(t, "worker-updated-title", current.Title)
	require.JSONEq(t, string(replacement), string(current.Metadata))
	require.Equal(t, types.ParseStatusCompleted, current.ParseStatus)
}
