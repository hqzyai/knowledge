package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type conversationSyncRepo struct {
	interfaces.KnowledgeRepository
	mu   sync.Mutex
	rows map[string]*types.Knowledge
}

func newConversationSyncRepo() *conversationSyncRepo {
	return &conversationSyncRepo{rows: make(map[string]*types.Knowledge)}
}

func cloneConversationKnowledge(in *types.Knowledge) *types.Knowledge {
	if in == nil {
		return nil
	}
	out := *in
	out.Metadata = append(types.JSON(nil), in.Metadata...)
	out.CustomMetadata = append(types.JSON(nil), in.CustomMetadata...)
	return &out
}

func (r *conversationSyncRepo) CreateKnowledge(_ context.Context, knowledge *types.Knowledge) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.rows[knowledge.ID]; exists {
		return errors.New("UNIQUE constraint failed: knowledges.id")
	}
	r.rows[knowledge.ID] = cloneConversationKnowledge(knowledge)
	return nil
}

func (r *conversationSyncRepo) GetKnowledgeByID(
	_ context.Context, tenantID uint64, id string,
) (*types.Knowledge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.rows[id]
	if row == nil || row.TenantID != tenantID {
		return nil, repository.ErrKnowledgeNotFound
	}
	return cloneConversationKnowledge(row), nil
}

func (r *conversationSyncRepo) CompareAndSwapKnowledgeMetadata(
	_ context.Context,
	tenantID uint64,
	id string,
	expected types.JSON,
	replacement types.JSON,
	values map[string]interface{},
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.rows[id]
	if row == nil || row.TenantID != tenantID || !bytes.Equal(row.Metadata, expected) {
		return false, nil
	}
	row.Metadata = append(types.JSON(nil), replacement...)
	applyConversationKnowledgeColumns(row, values)
	return true, nil
}

func (r *conversationSyncRepo) UpdateKnowledgeColumns(
	_ context.Context, id string, values map[string]interface{},
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row := r.rows[id]; row != nil {
		applyConversationKnowledgeColumns(row, values)
	}
	return nil
}

func applyConversationKnowledgeColumns(row *types.Knowledge, values map[string]interface{}) {
	for key, value := range values {
		switch key {
		case "parse_status":
			row.ParseStatus, _ = value.(string)
		case "summary_status":
			row.SummaryStatus, _ = value.(string)
		case "enable_status":
			row.EnableStatus, _ = value.(string)
		case "error_message":
			row.ErrorMessage, _ = value.(string)
		case "embedding_model_id":
			row.EmbeddingModelID, _ = value.(string)
		case "processed_at":
			if value == nil {
				row.ProcessedAt = nil
			}
		case "pending_subtasks_count":
			row.PendingSubtasksCount, _ = value.(int)
		case "updated_at":
			row.UpdatedAt, _ = value.(time.Time)
		}
	}
}

type conversationSyncKBService struct {
	interfaces.KnowledgeBaseService
	mu   sync.Mutex
	rows map[string]*types.KnowledgeBase
}

func newConversationSyncKBService() *conversationSyncKBService {
	return &conversationSyncKBService{rows: make(map[string]*types.KnowledgeBase)}
}

func (s *conversationSyncKBService) GetKnowledgeBaseByID(
	_ context.Context, id string,
) (*types.KnowledgeBase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kb := s.rows[id]; kb != nil {
		return kb, nil
	}
	return nil, repository.ErrKnowledgeBaseNotFound
}

func (s *conversationSyncKBService) CreateKnowledgeBase(
	ctx context.Context, kb *types.KnowledgeBase,
) (*types.KnowledgeBase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rows[kb.ID]; exists {
		return nil, errors.New("duplicate key value violates unique constraint")
	}
	kb.TenantID = types.MustTenantIDFromContext(ctx)
	kb.CreatorID, _ = types.UserIDFromContext(ctx)
	kb.SetStorageProvider("local")
	kb.EnsureDefaults()
	s.rows[kb.ID] = kb
	return kb, nil
}

type conversationSyncModelService struct {
	interfaces.ModelService
	models []*types.Model
}

func (s conversationSyncModelService) ListModels(context.Context) ([]*types.Model, error) {
	return s.models, nil
}

type conversationSyncTaskQueue struct {
	mu       sync.Mutex
	payloads []types.ManualProcessPayload
}

func (q *conversationSyncTaskQueue) Enqueue(
	task *asynq.Task, _ ...asynq.Option,
) (*asynq.TaskInfo, error) {
	var payload types.ManualProcessPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return nil, err
	}
	q.mu.Lock()
	q.payloads = append(q.payloads, payload)
	q.mu.Unlock()
	return &asynq.TaskInfo{ID: "task", Queue: "default", Type: task.Type()}, nil
}

func (q *conversationSyncTaskQueue) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.payloads)
}

func newConversationSyncTestService() (*knowledgeService, *conversationSyncRepo, *conversationSyncKBService, *conversationSyncTaskQueue) {
	repo := newConversationSyncRepo()
	kbs := newConversationSyncKBService()
	queue := &conversationSyncTaskQueue{}
	svc := &knowledgeService{
		repo:      repo,
		kbService: kbs,
		modelService: conversationSyncModelService{models: []*types.Model{
			{ID: "embedding-default", TenantID: 7, Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, IsDefault: true},
			{ID: "chat-default", TenantID: 7, Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive, IsDefault: true},
		}},
		task: queue,
	}
	return svc, repo, kbs, queue
}

func conversationSyncTestContext() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	return context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: 7})
}

func TestSyncConversationAppendsOneDailyDocumentAndConfiguresEnrichment(t *testing.T) {
	svc, repo, kbs, queue := newConversationSyncTestService()
	ctx := conversationSyncTestContext()
	at := time.Date(2026, 8, 17, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))

	first, err := svc.SyncConversation(ctx, &types.ConversationSyncRequest{
		UserID: "hermes-user-1", Text: "**问：** 第一问\n\n**答：** 第一答", ConversationAt: &at, EventID: "evt-1",
	})
	require.NoError(t, err)
	require.True(t, first.KnowledgeBaseCreated)
	require.True(t, first.DocumentCreated)
	require.Equal(t, 1, first.ContentVersion)
	require.Contains(t, ConversationKnowledgeBaseCandidateIDs(7, "hermes-user-1"), first.KnowledgeBaseID)

	secondAt := at.Add(2 * time.Hour)
	second, err := svc.SyncConversation(ctx, &types.ConversationSyncRequest{
		UserID: "hermes-user-1", Text: "**问：** 第二问\n\n**答：** 第二答", ConversationAt: &secondAt, EventID: "evt-2",
	})
	require.NoError(t, err)
	require.Equal(t, first.KnowledgeBaseID, second.KnowledgeBaseID)
	require.Equal(t, first.KnowledgeID, second.KnowledgeID)
	require.False(t, second.DocumentCreated)
	require.Equal(t, 2, second.ContentVersion)

	replay, err := svc.SyncConversation(ctx, &types.ConversationSyncRequest{
		UserID: "hermes-user-1", Text: "不应重复", ConversationAt: &secondAt, EventID: "evt-2",
	})
	require.NoError(t, err)
	require.True(t, replay.IdempotentReplay)
	require.Equal(t, 2, replay.ContentVersion)

	knowledge, err := repo.GetKnowledgeByID(ctx, 7, first.KnowledgeID)
	require.NoError(t, err)
	meta, err := knowledge.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, 2, meta.Version)
	require.Equal(t, types.ConversationSyncSourceHermes, meta.SyncSource)
	require.Equal(t, "2026-08-17", meta.DocumentDate)
	require.Equal(t, 1, bytes.Count([]byte(meta.Content), []byte("第一问")))
	require.Equal(t, 1, bytes.Count([]byte(meta.Content), []byte("第二问")))
	require.NotContains(t, meta.Content, "不应重复")
	require.Equal(t, 3, queue.count(), "an unindexed idempotent retry is safely re-enqueued")

	kb, err := kbs.GetKnowledgeBaseByID(ctx, first.KnowledgeBaseID)
	require.NoError(t, err)
	require.Equal(t, types.KnowledgeBaseTypeDocument, kb.Type)
	require.Equal(t, types.KnowledgeBaseVisibilityPersonal, kb.Visibility,
		"Hermes conversation knowledge bases must remain private to their creator by default")
	require.Equal(t, types.ExternalUserInternalID(7, "hermes-user-1"), kb.CreatorID)
	require.True(t, kb.IndexingStrategy.VectorEnabled)
	require.True(t, kb.IndexingStrategy.KeywordEnabled)
	require.True(t, kb.IndexingStrategy.WikiEnabled)
	require.True(t, kb.IndexingStrategy.GraphEnabled)
	require.NotNil(t, kb.ExtractConfig)
	require.True(t, kb.ExtractConfig.Enabled)
	require.NotNil(t, kb.QuestionGenerationConfig)
	require.True(t, kb.QuestionGenerationConfig.Enabled)
	require.Equal(t, 3, kb.QuestionGenerationConfig.QuestionCount)
	require.Equal(t, "chat-default", kb.WikiConfig.SynthesisModelID)
}

func TestSyncConversationConcurrentAppendsLoseNoContent(t *testing.T) {
	svc, repo, _, _ := newConversationSyncTestService()
	ctx := conversationSyncTestContext()
	at := time.Date(2026, 8, 17, 14, 0, 0, 0, time.Local)

	const calls = 20
	results := make(chan *types.ConversationSyncResult, calls)
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := svc.SyncConversation(ctx, &types.ConversationSyncRequest{
				UserID:         "parallel-user",
				Text:           "并发片段-" + time.Unix(int64(i), 0).UTC().Format("05"),
				ConversationAt: &at,
				EventID:        "parallel-event-" + time.Unix(int64(i), 0).UTC().Format("05"),
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		require.NoError(t, err)
	}

	var last *types.ConversationSyncResult
	for result := range results {
		last = result
	}
	require.NotNil(t, last)
	knowledge, err := repo.GetKnowledgeByID(ctx, 7, last.KnowledgeID)
	require.NoError(t, err)
	meta, err := knowledge.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, calls, meta.Version)
	for i := 0; i < calls; i++ {
		fragment := "并发片段-" + time.Unix(int64(i), 0).UTC().Format("05")
		require.Equal(t, 1, bytes.Count([]byte(meta.Content), []byte(fragment)), fragment)
	}
}
