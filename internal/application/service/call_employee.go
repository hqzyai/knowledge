package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/agentturn"
	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	maxCallableAgents        = 24
	maxAutoDiscoverEmployees = 48
	maxAnswerPreviewRunes    = 280
)

func lookupCallableAgent(ctx context.Context, repo interfaces.CustomAgentRepository, tenantID uint64, sourceID, id string) (*types.CustomAgent, error) {
	if id == "" || id == sourceID || types.IsBuiltinAgentID(id) {
		return nil, nil
	}
	target, err := repo.GetAgentByID(ctx, id, tenantID)
	if errors.Is(err, repository.ErrCustomAgentNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if target == nil || target.IsBuiltin || target.TenantID != tenantID {
		return nil, nil
	}
	return target, nil
}

func validateCallableAgents(ctx context.Context, repo interfaces.CustomAgentRepository, tenantID uint64, agent *types.CustomAgent) error {
	ids := dedupStrings(agent.Config.CallableAgentIDs)
	if len(ids) > maxCallableAgents {
		return apperrors.NewBadRequestError(fmt.Sprintf("Select at most %d callable employees", maxCallableAgents))
	}
	for _, id := range ids {
		target, err := lookupCallableAgent(ctx, repo, tenantID, agent.ID, id)
		if err != nil {
			return err
		}
		if target == nil {
			return apperrors.NewBadRequestError("Select another custom employee in this workspace")
		}
	}
	agent.Config.CallableAgentIDs = ids
	return nil
}

func truncateEmployeePreview(s string, maxRunes int) string {
	trimmed := strings.TrimSpace(s)
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}

// looksLikeLeakedToolMarkup detects models (e.g. DeepSeek DSML or XML-style
// tool tags) emitting raw tool-call markup as plain text instead of structured
// tool calls. When a delegated smart-reasoning employee hits this, falling back
// to quick-answer RAG produces a clean, grounded KB response.
func looksLikeLeakedToolMarkup(s string) bool {
	return strings.Contains(s, "DSML") ||
		strings.Contains(s, "<tool_requests>") ||
		strings.Contains(s, "<tool_call>") ||
		strings.Contains(s, "<invoke name=")
}

type employeeTool struct {
	tools.BaseTool
	sourceID     string
	autoAll      bool
	tenantID     uint64
	parentSessID string
	parentBus    *event.EventBus
	repo         interfaces.CustomAgentRepository
	sessions     interfaces.SessionService
	messages     interfaces.MessageService
	requestModel *types.RequestChatModel
	targetIDs    []string
	mu           sync.Mutex
	called       map[string]bool
}

func (s *sessionService) employeeTool(ctx context.Context, req *types.QARequest, parentBus *event.EventBus) (types.Tool, error) {
	if req.Delegated || req.SharedAgentReadOnly || req.DisableEmployeeDelegation {
		return nil, nil
	}
	principal, ok := types.PrincipalFromContext(ctx)
	if !ok || (principal.Type != types.PrincipalWebUser && principal.Type != types.PrincipalAPIExternalUser && principal.Type != types.PrincipalAPITenant) {
		return nil, nil
	}
	source := req.CustomAgent
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID != req.Session.TenantID || source.TenantID != tenantID || types.SessionOwnerIDFromContext(ctx) == "" {
		return nil, nil
	}

	autoAll := source.ID == types.BuiltinEmployeeRouterID
	var ids, labels []string
	if autoAll {
		allAgents, err := s.customAgents.ListAgentsByTenantID(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		customs := make([]*types.CustomAgent, 0, len(allAgents))
		for _, candidate := range allAgents {
			if candidate == nil || candidate.IsBuiltin || candidate.ID == "" || types.IsBuiltinAgentID(candidate.ID) || candidate.TenantID != tenantID {
				continue
			}
			customs = append(customs, candidate)
		}
		sort.Slice(customs, func(i, j int) bool {
			ni, nj := strings.ToLower(customs[i].Name), strings.ToLower(customs[j].Name)
			if ni != nj {
				return ni < nj
			}
			return customs[i].ID < customs[j].ID
		})
		if len(customs) > maxAutoDiscoverEmployees {
			customs = customs[:maxAutoDiscoverEmployees]
		}
		for _, target := range customs {
			ids = append(ids, target.ID)
			desc := strings.TrimSpace(target.Description)
			if desc == "" {
				desc = "通用企业数字员工"
			}
			labels = append(labels, fmt.Sprintf("- %s (employee_id=%s): %s", target.Name, target.ID, desc))
		}
	} else {
		for _, id := range source.Config.CallableAgentIDs {
			target, err := lookupCallableAgent(ctx, s.customAgents, tenantID, source.ID, id)
			if err != nil {
				return nil, err
			}
			if target == nil {
				continue
			}
			ids = append(ids, id)
			labels = append(labels, fmt.Sprintf("- %s (employee_id=%s): %s", target.Name, id, target.Description))
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}

	schema, err := json.Marshal(map[string]any{
		"type": "object", "required": []string{"employee_id", "question"}, "additionalProperties": false,
		"properties": map[string]any{
			"employee_id": map[string]any{"type": "string", "enum": ids},
			"question":    map[string]any{"type": "string", "description": "A self-contained task with the context the employee needs."},
		},
	})
	if err != nil {
		return nil, err
	}
	return &employeeTool{
		BaseTool:     tools.NewBaseTool(tools.ToolCallEmployee, "Dispatch a question to a matching digital employee and return their answer. Available digital employees:\n"+strings.Join(labels, "\n"), schema),
		repo:         s.customAgents,
		sessions:     s,
		messages:     s.turnMessages,
		sourceID:     source.ID,
		autoAll:      autoAll,
		tenantID:     tenantID,
		parentSessID: req.Session.ID,
		parentBus:    parentBus,
		targetIDs:    ids,
		requestModel: req.RequestChatModel,
	}, nil
}

func (t *employeeTool) Execute(ctx context.Context, raw json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID       string `json:"employee_id"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	question := strings.TrimSpace(args.Question)
	if question == "" {
		return nil, errors.New("employee question is required")
	}
	owner := types.SessionOwnerIDFromContext(ctx)
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID != t.tenantID || owner == "" || !slices.Contains(t.targetIDs, args.ID) {
		return nil, errors.New("employee is unavailable for this call")
	}
	if t.autoAll {
		t.mu.Lock()
		if t.called == nil {
			t.called = make(map[string]bool)
		}
		alreadyCalled := t.called[args.ID]
		t.called[args.ID] = true
		t.mu.Unlock()
		if alreadyCalled {
			return &types.ToolResult{
				Success: true,
				Output:  "This employee has already been consulted in this turn. Do not retry; synthesize the final answer from the information already gathered.",
				Data:    map[string]any{"employee_id": args.ID, "deduplicated": true},
			}, nil
		}
	}
	// Recheck source authorization when delegation is bound to a custom agent's list.
	if !t.autoAll {
		source, err := t.repo.GetAgentByID(ctx, t.sourceID, tenantID)
		if err != nil || source == nil || !source.IsAgentMode() || !slices.Contains(source.Config.CallableAgentIDs, args.ID) {
			return nil, errors.New("employee is no longer callable")
		}
	}
	target, err := lookupCallableAgent(ctx, t.repo, tenantID, t.sourceID, args.ID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, errors.New("employee is no longer available")
	}
	target.EnsureDefaults()
	session, err := t.sessions.CreateSession(ctx, &types.Session{
		TenantID: tenantID, UserID: owner, Title: target.Name,
		LastRequestState: &types.SessionLastRequestState{AgentID: target.ID},
	})
	if err != nil {
		return nil, err
	}
	// Delegated child sessions are internal execution scratchpads; soft-delete
	// them once the turn finishes so they never clutter the user's chat history.
	defer func() {
		if delErr := t.sessions.DeleteSession(context.WithoutCancel(ctx), session.ID); delErr != nil {
			logger.Warnf(ctx, "[call_employee] failed to clean up child session %s: %v", session.ID, delErr)
		}
	}()
	// A delegated answer belongs to the same caller-selected model route as the
	// parent turn. A nil override lets resolveChatModelID use the target's model.
	child := &types.QARequest{
		Session: session, Query: question, CustomAgent: target, Delegated: true,
		RequestChatModel: t.requestModel,
	}
	answer, refs, err := agentturn.Run(ctx, t.sessions, t.messages, child, "employee")
	if err != nil {
		return nil, err
	}
	// If a smart-reasoning employee leaked raw tool-call markup (e.g. DeepSeek
	// DSML or <tool_requests>) as plain text, fall back to quick-answer RAG so
	// the user still receives a clean, grounded knowledge-base answer.
	if target.IsAgentMode() && looksLikeLeakedToolMarkup(answer) {
		logger.Warnf(ctx, "[call_employee] detected leaked tool markup from %s (%s); retrying in quick-answer RAG mode", target.Name, target.ID)
		ragTarget := *target
		ragTarget.Config.AgentMode = types.AgentModeQuickAnswer
		ragChild := &types.QARequest{
			Session: session, Query: question, CustomAgent: &ragTarget, Delegated: true,
			RequestChatModel: t.requestModel,
		}
		if ragAnswer, ragRefs, ragErr := agentturn.Run(ctx, t.sessions, t.messages, ragChild, "employee"); ragErr == nil && strings.TrimSpace(ragAnswer) != "" && !looksLikeLeakedToolMarkup(ragAnswer) {
			answer = ragAnswer
			if len(ragRefs) > 0 {
				refs = ragRefs
			}
		}
	}
	// Propagate delegated knowledge references to the parent turn so citations
	// remain clickable in universal auto-dispatch conversations.
	if len(refs) > 0 && t.parentBus != nil {
		if emitErr := t.parentBus.Emit(ctx, event.Event{
			Type:      event.EventAgentReferences,
			SessionID: t.parentSessID,
			Data:      event.AgentReferencesData{References: refs},
		}); emitErr != nil {
			logger.Warnf(ctx, "[call_employee] failed to propagate references: %v", emitErr)
		}
	}
	return &types.ToolResult{
		Success: true,
		Output:  answer,
		Data: map[string]any{
			"employee_id":      target.ID,
			"employee_name":    target.Name,
			"employee_avatar":  target.Avatar,
			"question":         question,
			"session_id":       session.ID,
			"references_count": len(refs),
			"answer_preview":   truncateEmployeePreview(answer, maxAnswerPreviewRunes),
		},
	}, nil
}
