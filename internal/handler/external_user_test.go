package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type externalUserHandlerUserService struct {
	interfaces.UserService
	created bool
}

func (s *externalUserHandlerUserService) ProvisionExternalUser(
	_ context.Context, req *types.ExternalUserCreateRequest, tenantID uint64,
) (*types.User, bool, error) {
	created := !s.created
	s.created = true
	return &types.User{
		ID:       types.ExternalUserInternalID(tenantID, req.UserID),
		Username: req.Username,
		Email:    req.Email,
		TenantID: tenantID,
		IsActive: true,
	}, created, nil
}

type externalUserHandlerAPIKeyService struct {
	interfaces.TenantAPIKeyService
	key               *types.TenantAPIKey
	createErr         error
	keyAfterCreateErr *types.TenantAPIKey
	revoked           bool
}

func (s *externalUserHandlerAPIKeyService) RevokeAPIKey(
	_ context.Context, _ uint64, id uint64,
) error {
	if s.key != nil && s.key.ID == id {
		s.key = nil
		s.revoked = true
	}
	return nil
}

func (s *externalUserHandlerAPIKeyService) ListAPIKeys(
	_ context.Context, _ uint64,
) ([]*types.TenantAPIKey, error) {
	if s.key == nil {
		return nil, nil
	}
	return []*types.TenantAPIKey{s.key}, nil
}

func (s *externalUserHandlerAPIKeyService) CreateAPIKey(
	_ context.Context, req interfaces.TenantAPIKeyCreateRequest,
) (*interfaces.TenantAPIKeyCreateResult, error) {
	if s.createErr != nil {
		s.key = s.keyAfterCreateErr
		return nil, s.createErr
	}
	tenantID := req.TenantID
	s.key = &types.TenantAPIKey{
		ID: 91, TenantID: &tenantID, ScopeType: types.APIKeyScopeTenant,
		Name: req.Name, APIKey: "sk-external-user", FullAccess: req.FullAccess,
		Capabilities:     types.StringArray(req.Capabilities),
		KnowledgeBaseIDs: types.StringArray(req.KnowledgeBaseIDs),
	}
	return &interfaces.TenantAPIKeyCreateResult{APIKey: s.key, Token: s.key.APIKey}, nil
}

type externalUserHandlerTenantService struct{ interfaces.TenantService }
type externalUserHandlerMemberService struct{ interfaces.TenantMemberService }

func TestCreateExternalUserReturnsStableConversationKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userSvc := &externalUserHandlerUserService{}
	keySvc := &externalUserHandlerAPIKeyService{}
	h := &TenantHandler{
		service: externalUserHandlerTenantService{}, apiKeyService: keySvc,
		userService: userSvc, memberService: externalUserHandlerMemberService{},
	}
	engine := gin.New()
	engine.POST("/api/v1/external-users", func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, types.ExternalUserDefaultTenantID)
		c.Request = c.Request.WithContext(ctx)
		h.CreateExternalUser(c)
	})

	body := []byte(`{"user_id":"hermes-42","username":"hermes_42","email":"h42@example.com","password":"Hermes1234"}`)
	call := func() (int, types.ExternalUserCreateResponse) {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/external-users", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, req)
		var response types.ExternalUserCreateResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		return recorder.Code, response
	}

	status, first := call()
	require.Equal(t, http.StatusCreated, status)
	require.True(t, first.Success)
	require.Equal(t, uint64(10000), first.Data.SpaceID)
	require.Equal(t, types.TenantRoleContributor, first.Data.Role)
	require.Equal(t, "sk-external-user", first.Data.APIKey)
	require.False(t, first.Data.FullAccess)
	require.ElementsMatch(t, []string{"chat", "retrieve", "read_agents"}, []string(first.Data.Capabilities))
	require.Len(t, first.Data.KnowledgeBaseIDs, 100)
	require.Contains(t, first.Data.KnowledgeBaseIDs,
		service.ConversationKnowledgeBaseCandidateIDs(types.ExternalUserDefaultTenantID, "hermes-42")[0])
	keyCtx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
		KnowledgeBaseIDs: first.Data.KnowledgeBaseIDs,
		Capabilities:     first.Data.Capabilities,
	})
	require.NoError(t, types.AuthorizeTenantAPIKeyKnowledgeBases(
		keyCtx, first.Data.KnowledgeBaseIDs[0]))
	require.Error(t, types.AuthorizeTenantAPIKeyKnowledgeBases(keyCtx, "another-users-kb"))
	require.True(t, first.Data.UserCreated)
	require.True(t, first.Data.APIKeyCreated)

	status, replay := call()
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, first.Data.UserID, replay.Data.UserID)
	require.Equal(t, first.Data.APIKey, replay.Data.APIKey)
	require.False(t, replay.Data.UserCreated)
	require.False(t, replay.Data.APIKeyCreated)
}

func TestEnsureExternalUserConversationAPIKeyRecoversConcurrentWinner(t *testing.T) {
	tenantID := types.ExternalUserDefaultTenantID
	kbIDs := types.StringArray(service.ConversationKnowledgeBaseCandidateIDs(tenantID, "hermes-42"))
	winner := &types.TenantAPIKey{
		ID: 92, TenantID: &tenantID, ScopeType: types.APIKeyScopeTenant,
		Name: "external-user/internal-42", APIKey: "sk-race-winner", FullAccess: false,
		Capabilities:     types.StringArray{"chat", "retrieve", "read_agents"},
		KnowledgeBaseIDs: kbIDs,
	}
	keySvc := &externalUserHandlerAPIKeyService{
		createErr:         errors.New("unique constraint failed"),
		keyAfterCreateErr: winner,
	}
	h := &TenantHandler{apiKeyService: keySvc}

	key, created, err := h.ensureExternalUserConversationAPIKey(
		context.Background(), tenantID, "internal-42", "hermes-42")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, winner.ID, key.ID)
	require.Equal(t, "sk-race-winner", key.APIKey)
}

func TestEnsureExternalUserConversationAPIKeyRotatesLegacyFullAccessKey(t *testing.T) {
	tenantID := types.ExternalUserDefaultTenantID
	keySvc := &externalUserHandlerAPIKeyService{key: &types.TenantAPIKey{
		ID: 90, TenantID: &tenantID, ScopeType: types.APIKeyScopeTenant,
		Name: "external-user/internal-42", APIKey: "sk-legacy-full", FullAccess: true,
	}}
	h := &TenantHandler{apiKeyService: keySvc}

	key, created, err := h.ensureExternalUserConversationAPIKey(
		context.Background(), tenantID, "internal-42", "hermes-42")
	require.NoError(t, err)
	require.True(t, created)
	require.True(t, keySvc.revoked)
	require.False(t, key.FullAccess)
	require.ElementsMatch(t, []string{"chat", "retrieve", "read_agents"}, []string(key.Capabilities))
	require.Len(t, key.KnowledgeBaseIDs, 100)
}
