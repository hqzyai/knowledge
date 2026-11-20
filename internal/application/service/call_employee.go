package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/agentturn"
	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const maxCallableAgents = 24

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

type employeeTool struct {
	tools.BaseTool
	sourceID     string
	tenantID     uint64
	repo         interfaces.CustomAgentRepository
	sessions     interfaces.SessionService
	messages     interfaces.MessageService
	requestModel *types.RequestChatModel
	targetIDs    []string
}

func (s *sessionService) employeeTool(ctx context.Context, req *types.QARequest) (types.Tool, error) {
	if req.Delegated || req.SharedAgentReadOnly {
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
	var ids, labels []string
	for _, id := range source.Config.CallableAgentIDs {
		target, err := lookupCallableAgent(ctx, s.customAgents, tenantID, source.ID, id)
		if err != nil {
			return nil, err
		}
		if target == nil {
			continue
		}
		ids = append(ids, id)
		labels = append(labels, fmt.Sprintf("%s (%s): %s", target.Name, id, target.Description))
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
		BaseTool:     tools.NewBaseTool(tools.ToolCallEmployee, "Ask a configured employee to work on a task and return their answer. Available employees:\n"+strings.Join(labels, "\n"), schema),
		repo:         s.customAgents,
		sessions:     s,
		messages:     s.turnMessages,
		sourceID:     source.ID,
		tenantID:     tenantID,
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
	// Recheck both records so revocation or deletion takes effect during a long turn.
	source, err := t.repo.GetAgentByID(ctx, t.sourceID, tenantID)
	if err != nil || source == nil || !source.IsAgentMode() || !slices.Contains(source.Config.CallableAgentIDs, args.ID) {
		return nil, errors.New("employee is no longer callable")
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
	// A delegated answer belongs to the same caller-selected model route as the
	// parent turn. A nil override lets resolveChatModelID use the target's model.
	child := &types.QARequest{
		Session: session, Query: question, CustomAgent: target, Delegated: true,
		RequestChatModel: t.requestModel,
	}
	answer, _, err := agentturn.Run(ctx, t.sessions, t.messages, child, "employee")
	if err != nil {
		return nil, err
	}
	return &types.ToolResult{Success: true, Output: answer, Data: map[string]any{"employee_id": target.ID, "session_id": session.ID}}, nil
}
