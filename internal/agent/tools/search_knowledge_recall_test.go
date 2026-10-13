package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSearchKnowledgeRecallRecordsOnlyFinalResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scores []float64
		want   []types.RecallHit
	}{
		{"ranked and limited", []float64{0.9, 0.7, 0.02}, []types.RecallHit{{KnowledgeBaseID: "kb-1", ResourceID: "d1", Kind: "document"}}},
		{"all rejected", []float64{0.02, 0.03, 0.01}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubKnowledgeBaseService{results: []*types.SearchResult{
				{ID: "c1", KnowledgeID: "d1", KnowledgeBaseID: "kb-1", Content: "alpha", Score: 0.9},
				{ID: "c2", KnowledgeID: "d2", KnowledgeBaseID: "kb-1", Content: "beta", Score: 0.8},
				{ID: "c3", KnowledgeID: "d3", KnowledgeBaseID: "kb-1", Content: "gamma", Score: 0.7},
			}}
			tool := NewSearchKnowledgeTool(svc, nil, nil, types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 1}}, &stubReranker{scores: tc.scores}, &config.Config{Conversation: &config.ConversationConfig{RerankThreshold: 0.3}})
			recorder := &captureWikiRecall{}
			ctx := logger.CloneContext(types.WithRecallRecorder(context.Background(), recorder))
			res, err := tool.Execute(ctx, json.RawMessage(`{"query":"topic","limit":1}`))
			require.NoError(t, err)
			require.True(t, res.Success)
			require.Equal(t, tc.want, recorder.hits)
		})
	}
}
