package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type recallWikiService struct {
	interfaces.WikiPageService
	fail bool
}

func (s *recallWikiService) SearchPages(context.Context, string, string, int) ([]*types.WikiPage, error) {
	if s.fail {
		return nil, errors.New("search unavailable")
	}
	return []*types.WikiPage{{ID: "page", KnowledgeBaseID: "kb"}}, nil
}

type recallWikiKB struct {
	interfaces.KnowledgeBaseService
}

func (*recallWikiKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	kb := &types.KnowledgeBase{ID: "kb"}
	kb.IndexingStrategy.WikiEnabled = true
	return kb, nil
}

type recallWikiRecorder struct{ hits []types.RecallHit }

func (r *recallWikiRecorder) RecordRecall(_ context.Context, hits []types.RecallHit) {
	r.hits = append(r.hits, hits...)
}

func TestWikiHTTPSearchRecordsSuccessfulHitsOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, recorder := &recallWikiService{}, &recallWikiRecorder{}
	h := &WikiPageHandler{wikiService: svc, kbService: &recallWikiKB{}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(types.WithRecallRecorder(c.Request.Context(), recorder))
		c.Next()
	})
	router.GET("/wiki/:kb_id/search", h.SearchPages)
	for _, tc := range []struct {
		query  string
		fail   bool
		status int
		count  int
	}{
		{"?q=topic", false, 200, 1}, {"", false, 400, 0}, {"?q=topic", true, 500, 0},
	} {
		recorder.hits = nil
		svc.fail = tc.fail
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/wiki/kb/search"+tc.query, nil))
		require.Equal(t, tc.status, response.Code)
		require.Len(t, recorder.hits, tc.count)
		if tc.count > 0 {
			require.Equal(t, types.RecallHit{KnowledgeBaseID: "kb", ResourceID: "page", Kind: "wiki"}, recorder.hits[0])
		}
	}
}
