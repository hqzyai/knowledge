package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type captureWikiRecall struct{ hits []types.RecallHit }

func (r *captureWikiRecall) RecordRecall(_ context.Context, hits []types.RecallHit) {
	r.hits = append(r.hits, hits...)
}

func TestWikiRecallRecordsOnlyAuthorizedReturnedPagesAcrossDetachedContext(t *testing.T) {
	allowed := &types.WikiPage{ID: "allowed", KnowledgeBaseID: "kb", Slug: "concept/allowed", Title: "Allowed", PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"document"}}
	denied := &types.WikiPage{ID: "denied", KnowledgeBaseID: "kb", Slug: "concept/denied", PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"private-document"}}
	service := &fakeWikiPageService{searchResults: map[string][]*types.WikiPage{"kb": {allowed, denied}}}
	tool := NewWikiSearchTool(service, nil, []WikiScope{{KnowledgeBaseID: "kb", KnowledgeIDs: []string{"document"}}}, nil)
	recorder := &captureWikiRecall{}
	ctx := logger.CloneContext(types.WithRecallRecorder(context.Background(), recorder))
	result, err := tool.Execute(ctx, json.RawMessage(`{"queries":["topic"]}`))
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, []types.RecallHit{{KnowledgeBaseID: "kb", ResourceID: "allowed", Kind: "wiki"}}, recorder.hits)
	recorder.hits = nil
	result, err = tool.Execute(ctx, json.RawMessage(`{"queries":["topic"],"knowledge_base_id":"outside"}`))
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Empty(t, recorder.hits)
}
