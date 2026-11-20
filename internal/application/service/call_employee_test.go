package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type employeeTestRepo struct {
	interfaces.CustomAgentRepository
	agents map[string]*types.CustomAgent
}

func (r *employeeTestRepo) GetAgentByID(_ context.Context, id string, tenant uint64) (*types.CustomAgent, error) {
	if a := r.agents[id]; a != nil && a.TenantID == tenant {
		return a, nil
	}
	return nil, repository.ErrCustomAgentNotFound
}

type employeeTestSessions struct {
	interfaces.SessionService
	turn *types.QARequest
}

func (s *employeeTestSessions) CreateSession(_ context.Context, session *types.Session) (*types.Session, error) {
	session.ID = "separate-child-session"
	return session, nil
}
func (s *employeeTestSessions) KnowledgeQA(ctx context.Context, req *types.QARequest, bus *event.EventBus) error {
	s.turn = req
	return bus.EmitAndWait(ctx, event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{
		Content: "budget verified", Done: true,
	}})
}

type employeeTestMessages struct{ interfaces.MessageService }

func (employeeTestMessages) CreateMessage(_ context.Context, m *types.Message) (*types.Message, error) {
	m.ID = m.Role
	return m, nil
}
func (employeeTestMessages) UpdateMessage(context.Context, *types.Message) error { return nil }

func TestCallEmployee(t *testing.T) {
	ctx := types.WithPrincipal(context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7)), types.Principal{Type: types.PrincipalAPIExternalUser, ID: "7:person"})
	a := &types.CustomAgent{ID: "a", TenantID: 7, Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning, CallableAgentIDs: []string{"b"}}}
	b := &types.CustomAgent{ID: "b", Name: "Finance", TenantID: 7, Config: types.CustomAgentConfig{AgentMode: types.AgentModeQuickAnswer}}
	repo := &employeeTestRepo{agents: map[string]*types.CustomAgent{"a": a, "b": b, "foreign": {ID: "foreign", TenantID: 8}}}
	s := &sessionService{customAgents: repo, turnMessages: employeeTestMessages{}}
	req := &types.QARequest{Session: &types.Session{ID: "parent", TenantID: 7}, CustomAgent: a, RequestChatModel: &types.RequestChatModel{ModelName: "api-model"}}

	for _, bad := range []string{"a", "missing", "foreign", types.BuiltinQuickAnswerID} {
		copy := *a
		copy.Config.CallableAgentIDs = []string{bad}
		require.Error(t, validateCallableAgents(ctx, repo, 7, &copy))
	}
	require.NoError(t, validateCallableAgents(ctx, repo, 7, a))
	tool, err := s.employeeTool(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, tool)
	require.Contains(t, string(tool.Parameters()), `"enum":["b"]`)
	runner := &employeeTestSessions{}
	tool.(*employeeTool).sessions = runner
	call := func(id string) (*types.ToolResult, error) {
		args, err := json.Marshal(map[string]string{"employee_id": id, "question": "Review budget"})
		require.NoError(t, err)
		return tool.Execute(ctx, args)
	}
	_, err = call("foreign")
	require.Error(t, err)
	result, err := call("b")
	require.NoError(t, err)
	require.Equal(t, "budget verified", result.Output)
	require.Equal(t, types.SessionOwnerIDFromContext(ctx), runner.turn.Session.UserID)
	require.NotEqual(t, req.Session.ID, runner.turn.Session.ID)
	require.Same(t, b, runner.turn.CustomAgent)
	require.Same(t, req.RequestChatModel, runner.turn.RequestChatModel)
	require.True(t, runner.turn.Delegated)

	b.Config.ModelID = "configured"
	_, err = call("b")
	require.NoError(t, err)
	require.Same(t, req.RequestChatModel, runner.turn.RequestChatModel)
	require.Equal(t, "configured", runner.turn.CustomAgent.Config.ModelID)

	req.RequestChatModel = nil
	tool, err = s.employeeTool(ctx, req)
	require.NoError(t, err)
	tool.(*employeeTool).sessions = runner
	_, err = call("b")
	require.NoError(t, err)
	require.Nil(t, runner.turn.RequestChatModel)
	require.Equal(t, "configured", runner.turn.CustomAgent.Config.ModelID)
	a.Config.CallableAgentIDs = nil
	_, err = call("b")
	require.Error(t, err)
	a.Config.CallableAgentIDs = []string{"b"}
	req.Delegated = true
	nested, err := s.employeeTool(ctx, req)
	require.NoError(t, err)
	require.Nil(t, nested)
	req.Delegated = false
	req.SharedAgentReadOnly = true
	shared, err := s.employeeTool(ctx, req)
	require.NoError(t, err)
	require.Nil(t, shared)
}
