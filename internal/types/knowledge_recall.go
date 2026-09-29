package types

import "context"

// RecallHit is a returned resource, not an intermediate vector candidate or a page view.
type RecallHit struct {
	KnowledgeBaseID string
	ResourceID      string
	Kind            string
}

type RecallRecorder interface {
	RecordRecall(context.Context, []RecallHit)
}

const RecallRecorderContextKey ContextKey = "RecallRecorder"

func WithRecallRecorder(ctx context.Context, recorder RecallRecorder) context.Context {
	return context.WithValue(ctx, RecallRecorderContextKey, recorder)
}

func RecordRecall(ctx context.Context, hits []RecallHit) {
	if recorder, ok := ctx.Value(RecallRecorderContextKey).(RecallRecorder); ok && recorder != nil && len(hits) > 0 {
		recorder.RecordRecall(ctx, hits)
	}
}

func RecordSearchRecall(ctx context.Context, results []*SearchResult) {
	hits := make([]RecallHit, 0, len(results))
	for _, result := range results {
		if result == nil || result.KnowledgeID == "" {
			continue
		}
		kind := "document"
		if result.ChunkType == ChunkTypeWikiPage {
			kind = "wiki"
		}
		hits = append(hits, RecallHit{KnowledgeBaseID: result.KnowledgeBaseID, ResourceID: result.KnowledgeID, Kind: kind})
	}
	RecordRecall(ctx, hits)
}
