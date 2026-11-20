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

func (r *employeeTestRepo) ListAgentsByTenantID(_ context.Context, tenant uint64) ([]*types.CustomAgent, error) {
	var list []*types.CustomAgent
	for _, a := range r.agents {
		if a != nil && a.TenantID == tenant {
			list = append(list, a)
		}
	}
	return list, nil
}

type employeeTestSessions struct {
	interfaces.SessionService
	turn *types.QARequest
	refs []*types.SearchResult
}

func (s *employeeTestSessions) CreateSession(_ context.Context, session *types.Session) (*types.Session, error) {
	session.ID = "separate-child-session"
	return session, nil
}
func (s *employeeTestSessions) DeleteSession(_ context.Context, _ string) error {
	return nil
}
func (s *employeeTestSessions) KnowledgeQA(ctx context.Context, req *types.QARequest, bus *event.EventBus) error {
	s.turn = req
	if len(s.refs) > 0 {
		_ = bus.EmitAndWait(ctx, event.Event{Type: event.EventAgentReferences, Data: event.AgentReferencesData{
			References: s.refs,
		}})
	}
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
	b := &types.CustomAgent{ID: "b", Name: "Finance", Avatar: "💰", TenantID: 7, Config: types.CustomAgentConfig{AgentMode: types.AgentModeQuickAnswer}}
	repo := &employeeTestRepo{agents: map[string]*types.CustomAgent{"a": a, "b": b, "foreign": {ID: "foreign", TenantID: 8}}}
	s := &sessionService{customAgents: repo, turnMessages: employeeTestMessages{}}
	req := &types.QARequest{Session: &types.Session{ID: "parent", TenantID: 7}, CustomAgent: a, RequestChatModel: &types.RequestChatModel{ModelName: "api-model"}}

	for _, bad := range []string{"a", "missing", "foreign", types.BuiltinQuickAnswerID} {
		copy := *a
		copy.Config.CallableAgentIDs = []string{bad}
		require.Error(t, validateCallableAgents(ctx, repo, 7, &copy))
	}
	require.NoError(t, validateCallableAgents(ctx, repo, 7, a))
	tool, err := s.employeeTool(ctx, req, nil)
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
	require.Equal(t, "Finance", result.Data["employee_name"])
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
	tool, err = s.employeeTool(ctx, req, nil)
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
	nested, err := s.employeeTool(ctx, req, nil)
	require.NoError(t, err)
	require.Nil(t, nested)
	req.Delegated = false
	req.SharedAgentReadOnly = true
	shared, err := s.employeeTool(ctx, req, nil)
	require.NoError(t, err)
	require.Nil(t, shared)
	req.SharedAgentReadOnly = false
	req.DisableEmployeeDelegation = true
	disabled, err := s.employeeTool(ctx, req, nil)
	require.NoError(t, err)
	require.Nil(t, disabled)
}

func TestBuiltinEmployeeRouterAutoDiscoveryAndReferences(t *testing.T) {
	ctx := types.WithPrincipal(context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7)), types.Principal{Type: types.PrincipalAPIExternalUser, ID: "7:person"})
	router := types.GetBuiltinAgent(types.BuiltinEmployeeRouterID, 7)
	require.NotNil(t, router)
	require.True(t, router.IsAgentMode())
	require.True(t, types.IsBuiltinAgentID(types.BuiltinEmployeeRouterID))
	require.NotContains(t, types.GetBuiltinAgentIDs(), types.BuiltinEmployeeRouterID)

	hr := &types.CustomAgent{ID: "hr", Name: "HR 助手", Description: "处理考勤与入职", Avatar: "👥", TenantID: 7, Config: types.CustomAgentConfig{AgentMode: types.AgentModeQuickAnswer}}
	fin := &types.CustomAgent{ID: "fin", Name: "财务助手", Description: "报销与预算", Avatar: "💰", TenantID: 7, Config: types.CustomAgentConfig{AgentMode: types.AgentModeQuickAnswer}}
	builtinOther := &types.CustomAgent{ID: types.BuiltinQuickAnswerID, Name: "Quick", IsBuiltin: true, TenantID: 7}
	foreign := &types.CustomAgent{ID: "other-tenant", Name: "Foreign", TenantID: 9}
	repo := &employeeTestRepo{agents: map[string]*types.CustomAgent{
		"hr": hr, "fin": fin, builtinOther.ID: builtinOther, "other-tenant": foreign,
	}}
	s := &sessionService{customAgents: repo, turnMessages: employeeTestMessages{}}

	parentBus := event.NewEventBus()
	var propagated int
	parentBus.On(event.EventAgentReferences, func(_ context.Context, evt event.Event) error {
		if data, ok := evt.Data.(event.AgentReferencesData); ok {
			if list, ok := data.References.([]*types.SearchResult); ok {
				propagated += len(list)
			}
		}
		return nil
	})

	req := &types.QARequest{
		Session:          &types.Session{ID: "parent-auto", TenantID: 7},
		CustomAgent:      router,
		RequestChatModel: &types.RequestChatModel{ModelName: "api-model"},
	}
	tool, err := s.employeeTool(ctx, req, parentBus)
	require.NoError(t, err)
	require.NotNil(t, tool)
	params := string(tool.Parameters())
	require.Contains(t, params, `"hr"`)
	require.Contains(t, params, `"fin"`)
	require.NotContains(t, params, `"other-tenant"`)
	require.NotContains(t, params, types.BuiltinQuickAnswerID)

	runner := &employeeTestSessions{
		refs: []*types.SearchResult{{KnowledgeTitle: "员工手册", Content: "报销流程说明"}},
	}
	tool.(*employeeTool).sessions = runner

	args, err := json.Marshal(map[string]string{"employee_id": "fin", "question": "差旅报销标准是什么？"})
	require.NoError(t, err)
	res, err := tool.Execute(ctx, args)
	require.NoError(t, err)
	require.Equal(t, "budget verified", res.Output)
	require.Equal(t, "fin", res.Data["employee_id"])
	require.Equal(t, "财务助手", res.Data["employee_name"])
	require.Equal(t, 1, res.Data["references_count"])
	require.Equal(t, 1, propagated)
}
