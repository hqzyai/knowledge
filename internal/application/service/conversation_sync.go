package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/application/repository"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
)

const (
	conversationSyncMaxUserIDLen   = 128
	conversationSyncMaxEventIDLen  = 256
	conversationSyncMaxEventIDs    = 1000
	conversationSyncResourceSlots  = 100
	conversationSyncCASMaxAttempts = 32
)

// SyncConversation is the Hermes-facing ingestion primitive. Resources have
// deterministic IDs, which gives first-call creation an idempotent uniqueness
// boundary without a separate mapping table. Daily content is replaced with
// metadata CAS, so concurrent deliveries publish one complete snapshot.
func (s *knowledgeService) SyncConversation(
	ctx context.Context, request *types.ConversationSyncRequest,
) (*types.ConversationSyncResult, error) {
	if request == nil {
		return nil, werrors.NewBadRequestError("请求内容不能为空")
	}
	userID, title, qaContent, eventID, err := validateConversationSyncRequest(request)
	if err != nil {
		return nil, err
	}

	tenantID := types.MustTenantIDFromContext(ctx)
	conversationAt := time.Now()
	if request.ConversationAt != nil {
		conversationAt = *request.ConversationAt
	}
	conversationAt = conversationAt.In(time.Local)
	documentDate := conversationAt.Format("2006-01-02")

	kb, kbCreated, err := s.ensureConversationKnowledgeBase(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.checkStorageEngineConfigured(ctx, kb); err != nil {
		return nil, err
	}

	knowledge, documentCreated, replay, err := s.upsertDailyConversation(
		ctx, tenantID, kb, userID, title, documentDate, qaContent, eventID)
	if err != nil {
		return nil, err
	}
	meta, err := knowledge.ManualMetadata()
	if err != nil || meta == nil {
		return nil, fmt.Errorf("read conversation document metadata: %w", err)
	}

	// A replay may be the retry of a request whose first queue submission
	// failed. Re-enqueue whenever this revision has not reached primary indexing;
	// the revision-aware worker makes duplicate submissions harmless.
	if meta.IndexedVersion < meta.Version {
		if knowledge.ParseStatus == types.ParseStatusFailed {
			knowledge.ParseStatus = types.ParseStatusPending
			knowledge.ErrorMessage = ""
			knowledge.ProcessedAt = nil
			if err := s.repo.UpdateKnowledgeColumns(ctx, knowledge.ID, map[string]interface{}{
				"parse_status":           types.ParseStatusPending,
				"error_message":          "",
				"processed_at":           nil,
				"pending_subtasks_count": 0,
				"updated_at":             time.Now(),
			}); err != nil {
				return nil, err
			}
		}
		if _, err := s.enqueueManualProcessing(ctx, knowledge, meta.Content, !documentCreated); err != nil {
			_ = s.repo.UpdateKnowledgeColumns(ctx, knowledge.ID, map[string]interface{}{
				"parse_status":  types.ParseStatusFailed,
				"error_message": "failed to enqueue conversation processing task",
				"updated_at":    time.Now(),
			})
			return nil, fmt.Errorf("enqueue conversation processing: %w", err)
		}
		knowledge.ParseStatus = types.ParseStatusPending
	}

	return &types.ConversationSyncResult{
		KnowledgeBaseID:      kb.ID,
		KnowledgeID:          knowledge.ID,
		DocumentDate:         documentDate,
		ContentVersion:       meta.Version,
		ParseStatus:          knowledge.ParseStatus,
		KnowledgeBaseCreated: kbCreated,
		DocumentCreated:      documentCreated,
		IdempotentReplay:     replay,
	}, nil
}

func validateConversationSyncRequest(
	request *types.ConversationSyncRequest,
) (userID string, title string, qaContent string, eventID string, err error) {
	userID = strings.TrimSpace(request.UserID)
	if userID == "" {
		return "", "", "", "", werrors.NewValidationError("user_id 不能为空")
	}
	if !utf8.ValidString(userID) || len([]rune(userID)) > conversationSyncMaxUserIDLen {
		return "", "", "", "", werrors.NewValidationError("user_id 非法或过长")
	}
	if _, ok := secutils.ValidateInput(userID); !ok {
		return "", "", "", "", werrors.NewValidationError("user_id 包含非法字符")
	}
	for _, r := range userID {
		if r < 0x20 || r == 0x7f {
			return "", "", "", "", werrors.NewValidationError("user_id 包含控制字符")
		}
	}

	title = strings.TrimSpace(request.Title)
	if title == "" {
		return "", "", "", "", werrors.NewValidationError("title 不能为空")
	}
	if safe, ok := secutils.ValidateInput(title); !ok {
		return "", "", "", "", werrors.NewValidationError("title 包含非法字符")
	} else {
		title = safe
	}
	for _, r := range title {
		if r < 0x20 || r == 0x7f {
			return "", "", "", "", werrors.NewValidationError("title 包含控制字符")
		}
	}

	qaContent = strings.TrimSpace(secutils.CleanMarkdown(request.Text))
	if qaContent == "" {
		return "", "", "", "", werrors.NewValidationError("text 不能为空")
	}
	if safe, ok := secutils.ValidateInput(qaContent); !ok {
		return "", "", "", "", werrors.NewValidationError("text 包含非法字符")
	} else {
		qaContent = safe
	}
	if len([]rune(qaContent)) > manualContentMaxLength {
		return "", "", "", "", werrors.NewValidationError(
			fmt.Sprintf("text 超出长度限制（最多%d个字符）", manualContentMaxLength))
	}

	eventID = strings.TrimSpace(request.EventID)
	if !utf8.ValidString(eventID) || len([]rune(eventID)) > conversationSyncMaxEventIDLen {
		return "", "", "", "", werrors.NewValidationError("event_id 过长")
	}
	for _, r := range eventID {
		if r < 0x20 || r == 0x7f {
			return "", "", "", "", werrors.NewValidationError("event_id 包含控制字符")
		}
	}
	return userID, title, qaContent, eventID, nil
}

func (s *knowledgeService) ensureConversationKnowledgeBase(
	ctx context.Context, tenantID uint64, userID string,
) (*types.KnowledgeBase, bool, error) {
	var embeddingModelID, summaryModelID string
	modelsSelected := false
	for slot := 0; slot < conversationSyncResourceSlots; slot++ {
		id := conversationSyncDeterministicID("knowledge-base", tenantID, userID, "", slot)
		kb, err := s.kbService.GetKnowledgeBaseByID(ctx, id)
		if err == nil && kb != nil {
			if kb.TenantID != tenantID {
				return nil, false, werrors.NewForbiddenError("知识库不属于当前空间")
			}
			return kb, false, nil
		}
		if err != nil && !errors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			return nil, false, err
		}
		if !modelsSelected {
			embeddingModelID, summaryModelID, err = s.selectConversationModels(ctx, tenantID)
			if err != nil {
				return nil, false, err
			}
			modelsSelected = true
		}

		candidate := &types.KnowledgeBase{
			ID:               id,
			CreatorID:        types.ExternalUserInternalID(tenantID, userID),
			Visibility:       types.KnowledgeBaseVisibilityPersonal,
			Name:             conversationKnowledgeBaseName(userID),
			Description:      "Hermes 自动同步的用户私有对话知识库",
			Type:             types.KnowledgeBaseTypeDocument,
			EmbeddingModelID: embeddingModelID,
			SummaryModelID:   summaryModelID,
			ChunkingConfig: types.ChunkingConfig{
				ChunkSize:         512,
				ChunkOverlap:      80,
				Separators:        []string{"\n\n", "\n", "。", "！", "？", ";", "；"},
				EnableParentChild: true,
				ParentChunkSize:   4096,
				ChildChunkSize:    384,
				Strategy:          "auto",
			},
			QuestionGenerationConfig: &types.QuestionGenerationConfig{
				Enabled:       true,
				QuestionCount: 3,
			},
			WikiConfig:    &types.WikiConfig{SynthesisModelID: summaryModelID},
			ExtractConfig: &types.ExtractConfig{Enabled: false},
			IndexingStrategy: types.IndexingStrategy{
				VectorEnabled:  true,
				KeywordEnabled: true,
				WikiEnabled:    true,
				GraphEnabled:   false,
			},
		}
		// Conversation ingestion is authenticated only at the endpoint layer.
		// Stamp the deterministic external account as creator explicitly instead
		// of inheriting the tenant API key's synthetic/owner identity.
		creatorCtx := context.WithValue(
			ctx,
			types.UserIDContextKey,
			types.ExternalUserInternalID(tenantID, userID),
		)
		created, createErr := s.kbService.CreateKnowledgeBase(creatorCtx, candidate)
		if createErr == nil {
			return created, true, nil
		}
		if !isUniqueViolation(createErr) {
			return nil, false, createErr
		}
		// A concurrent request may have created this candidate. If instead
		// the deterministic ID belongs to a soft-deleted resource, advance
		// to the next deterministic slot so deletion remains recoverable.
		if winner, getErr := s.kbService.GetKnowledgeBaseByID(ctx, id); getErr == nil && winner != nil {
			return winner, false, nil
		}
	}
	return nil, false, werrors.NewConflictError("无法为用户分配对话知识库")
}

func (s *knowledgeService) selectConversationModels(
	ctx context.Context, tenantID uint64,
) (embeddingModelID string, summaryModelID string, err error) {
	models, err := s.modelService.ListModels(ctx)
	if err != nil {
		return "", "", err
	}
	selectModel := func(modelType types.ModelType) string {
		for _, preferDefault := range []bool{true, false} {
			for _, preferTenant := range []bool{true, false} {
				for _, model := range models {
					if model == nil || model.Type != modelType || model.Status != types.ModelStatusActive {
						continue
					}
					if model.IsDefault != preferDefault || (model.TenantID == tenantID) != preferTenant {
						continue
					}
					return model.ID
				}
			}
		}
		return ""
	}
	embeddingModelID = selectModel(types.ModelTypeEmbedding)
	summaryModelID = selectModel(types.ModelTypeKnowledgeQA)
	if embeddingModelID == "" || summaryModelID == "" {
		return "", "", werrors.NewValidationError("请先为当前空间配置可用的向量模型和知识问答模型")
	}
	return embeddingModelID, summaryModelID, nil
}

func (s *knowledgeService) upsertDailyConversation(
	ctx context.Context,
	tenantID uint64,
	kb *types.KnowledgeBase,
	userID string,
	title string,
	documentDate string,
	qaContent string,
	eventID string,
) (*types.Knowledge, bool, bool, error) {
	eventHash := conversationSyncEventHash(tenantID, userID, eventID)
	content := formatConversationMarkdownDocument(title, qaContent)

	for slot := 0; slot < conversationSyncResourceSlots; slot++ {
		id := conversationSyncDeterministicID("document", tenantID, userID, documentDate, slot)
		for casAttempt := 0; casAttempt < conversationSyncCASMaxAttempts; casAttempt++ {
			knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, id)
			if errors.Is(err, repository.ErrKnowledgeNotFound) {
				meta := types.NewManualKnowledgeMetadata(content, types.ManualKnowledgeStatusPublish, 1)
				meta.SyncSource = types.ConversationSyncSourceHermes
				meta.ExternalUserID = userID
				meta.DocumentDate = documentDate
				if eventHash != "" {
					meta.SyncEventIDs = []string{eventHash}
				}
				candidate := &types.Knowledge{
					ID:               id,
					TenantID:         tenantID,
					KnowledgeBaseID:  kb.ID,
					Type:             types.KnowledgeTypeManual,
					Title:            title,
					Source:           types.KnowledgeTypeManual,
					Channel:          types.ChannelAPI,
					ParseStatus:      types.ParseStatusPending,
					SummaryStatus:    types.SummaryStatusNone,
					EnableStatus:     "disabled",
					EmbeddingModelID: kb.EmbeddingModelID,
					FileName:         ensureManualFileName(title),
					FileType:         types.KnowledgeTypeManual,
					CreatedAt:        time.Now(),
					UpdatedAt:        time.Now(),
				}
				if err := candidate.SetManualMetadata(meta); err != nil {
					return nil, false, false, err
				}
				if createErr := s.repo.CreateKnowledge(ctx, candidate); createErr == nil {
					return candidate, true, false, nil
				} else if !isUniqueViolation(createErr) {
					return nil, false, false, createErr
				}
				// Concurrent create or a soft-deleted deterministic ID. Re-read;
				// if still absent, move to the next slot.
				if winner, winnerErr := s.repo.GetKnowledgeByID(ctx, tenantID, id); winnerErr == nil && winner != nil {
					continue
				}
				break
			}
			if err != nil {
				return nil, false, false, err
			}
			if knowledge.KnowledgeBaseID != kb.ID || !knowledge.IsManual() {
				break
			}
			meta, err := knowledge.ManualMetadata()
			if err != nil || meta == nil || meta.SyncSource != types.ConversationSyncSourceHermes ||
				meta.ExternalUserID != userID || meta.DocumentDate != documentDate {
				break
			}
			if eventHash != "" && containsConversationSyncEvent(meta.SyncEventIDs, eventHash) {
				return knowledge, false, true, nil
			}

			if len([]rune(content)) > manualContentMaxLength {
				return nil, false, false, werrors.NewValidationError(
					fmt.Sprintf("%s 的当日对话文档已达到%d字符上限", documentDate, manualContentMaxLength))
			}
			expected := append(types.JSON(nil), knowledge.Metadata...)
			meta.Content = content
			meta.Status = types.ManualKnowledgeStatusPublish
			meta.Version++
			meta.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if eventHash != "" {
				meta.SyncEventIDs = append(meta.SyncEventIDs, eventHash)
				if len(meta.SyncEventIDs) > conversationSyncMaxEventIDs {
					meta.SyncEventIDs = meta.SyncEventIDs[len(meta.SyncEventIDs)-conversationSyncMaxEventIDs:]
				}
			}
			replacement, err := meta.ToJSON()
			if err != nil {
				return nil, false, false, err
			}
			updated, err := s.repo.CompareAndSwapKnowledgeMetadata(
				ctx, tenantID, knowledge.ID, expected, replacement, map[string]interface{}{
					"title":                  title,
					"file_name":              ensureManualFileName(title),
					"parse_status":           types.ParseStatusPending,
					"summary_status":         types.SummaryStatusNone,
					"enable_status":          "disabled",
					"error_message":          "",
					"processed_at":           nil,
					"pending_subtasks_count": 0,
					"embedding_model_id":     kb.EmbeddingModelID,
					"updated_at":             time.Now(),
				})
			if err != nil {
				return nil, false, false, err
			}
			if !updated {
				continue
			}
			knowledge.Metadata = replacement
			knowledge.Title = title
			knowledge.FileName = ensureManualFileName(title)
			knowledge.ParseStatus = types.ParseStatusPending
			knowledge.SummaryStatus = types.SummaryStatusNone
			knowledge.ProcessedAt = nil
			return knowledge, false, false, nil
		}
		if active, err := s.repo.GetKnowledgeByID(ctx, tenantID, id); err == nil && active != nil {
			// CAS contention exhausted on a valid active document. Do not move
			// to another slot and split one calendar day across two documents.
			return nil, false, false, werrors.NewConflictError("当日对话正在并发更新，请重试")
		}
	}
	return nil, false, false, werrors.NewConflictError("无法为当日对话分配文档")
}

func conversationSyncDeterministicID(
	kind string, tenantID uint64, userID string, documentDate string, slot int,
) string {
	seed := fmt.Sprintf("weknora:conversation-sync:v1:%s:%d:%s:%s:%d",
		kind, tenantID, userID, documentDate, slot)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(seed)).String()
}

// ConversationKnowledgeBaseCandidateIDs returns every deterministic KB ID
// that conversation sync may allocate for one user. Provisioned chat keys use
// this as their static KB allow-list, including recovery slots for a previously
// soft-deleted deterministic ID.
func ConversationKnowledgeBaseCandidateIDs(tenantID uint64, userID string) []string {
	userID = strings.TrimSpace(userID)
	ids := make([]string, 0, conversationSyncResourceSlots)
	for slot := 0; slot < conversationSyncResourceSlots; slot++ {
		ids = append(ids, conversationSyncDeterministicID("knowledge-base", tenantID, userID, "", slot))
	}
	return ids
}

func conversationKnowledgeBaseName(userID string) string {
	name := "Hermes 对话 · " + userID
	runes := []rune(name)
	if len(runes) > 200 {
		name = string(runes[:200])
	}
	return name
}

func formatConversationMarkdownDocument(title, qaContent string) string {
	return fmt.Sprintf("# %s\n\n%s", title, strings.TrimSpace(qaContent))
}

func conversationSyncEventHash(tenantID uint64, userID, eventID string) string {
	if eventID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", tenantID, userID, eventID)))
	return hex.EncodeToString(sum[:16])
}

func containsConversationSyncEvent(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
