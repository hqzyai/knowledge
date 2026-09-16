package middleware

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type consoleUsers struct {
	interfaces.UserService
	user *types.User
}

func (s *consoleUsers) GetUserByTenantID(context.Context, uint64) (*types.User, error) {
	return s.user, nil
}

func (s *consoleUsers) GetUserByID(context.Context, string) (*types.User, error) { return s.user, nil }

type consoleTenants struct{ interfaces.TenantService }

func (s *consoleTenants) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 10000, Status: "active"}, nil
}

type consoleKeys struct {
	interfaces.TenantAPIKeyService
	key *types.TenantAPIKey
}

func (s *consoleKeys) AuthenticateAPIKey(context.Context, string) (*types.TenantAPIKey, error) {
	return s.key, nil
}

func TestAgentOSConsoleRequestBoundUserAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	require.NoError(t, err)
	t.Setenv("AGENTOS_CONSOLE_PUBLIC_KEY", base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})))
	server := miniredis.RunT(t)
	cache := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	userID := types.ExternalUserInternalID(10000, "employee-1")
	users := &consoleUsers{user: &types.User{ID: userID, IsActive: true, Preferences: types.UserPreferences{MustChangePassword: true}}}
	tenantID := uint64(10000)
	keys := &consoleKeys{key: &types.TenantAPIKey{TenantID: &tenantID, Name: types.ExternalUserAPIKeyNamePrefix + userID}}
	members := newFakeMemberService()
	members.seedActive(userID, 10000, types.TenantRoleContributor)
	cfg := &config.Config{Tenant: &config.TenantConfig{}}
	enabled := true
	cfg.Tenant.EnableRBAC = &enabled
	engine := gin.New()
	engine.Use(ErrorHandler())
	engine.Use(AgentOSConsoleAuth(&consoleTenants{}, users, members, keys, cache, cfg))
	engine.Use(Auth(&consoleTenants{}, users, members, keys, cfg))
	policy := NewAPIKeyRouteAuthorizer()
	policy.Register("POST", "/api/v1/knowledge-bases", APIKeyRoutePolicy{RequireFullAccess: true})
	engine.Use(policy.Middleware())
	engine.POST("/api/v1/knowledge-bases", RequireRole(types.TenantRoleContributor, cfg), func(c *gin.Context) {
		_, machine := types.TenantAPIKeyScopeFromContext(c.Request.Context())
		require.False(t, machine)
		got, _ := types.UserIDFromContext(c.Request.Context())
		require.Equal(t, userID, got)
		c.Status(201)
	})
	engine.PUT("/api/v1/knowledge-bases/:id", RequireOwnershipOrRole(types.TenantRoleAdmin, func(c *gin.Context) (string, error) {
		if c.Param("id") == "mine" {
			return userID, nil
		}
		return "colleague", nil
	}, cfg), func(c *gin.Context) { c.Status(204) })
	sharedLookup := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"shared": {ID: "shared", TenantID: 42, CreatorID: "colleague"}}}
	sharedGrant := &stubKBShareForGuard{permission: map[string]types.OrgMemberRole{"shared": types.OrgRoleViewer}, shared: map[string]bool{"shared": true}, source: map[string]uint64{"shared": 42}}
	engine.GET("/api/v1/knowledgebase/:kb_id/wiki/stats", RequireKBAccess(KBIDFromParam("kb_id"), types.OrgRoleViewer, sharedLookup, sharedGrant, nil, cfg), func(c *gin.Context) { c.Status(200) })
	engine.POST("/api/v1/knowledgebase/:kb_id/wiki/auto-fix", RequireKBAccess(KBIDFromParam("kb_id"), types.OrgRoleEditor, sharedLookup, sharedGrant, nil, cfg), func(c *gin.Context) { c.Status(201) })
	engine.POST("/api/v1/system/admin/users", func(c *gin.Context) { c.Status(201) })
	engine.GET("/api/v1/tenants/kv/:key", func(c *gin.Context) { c.Status(200) })
	engine.GET("/api/v1/system/parser-engines", func(c *gin.Context) { c.Status(200) })
	engine.POST("/api/v1/initialization/extract/text-relation", RequireRole(types.TenantRoleAdmin, cfg), func(c *gin.Context) { c.Status(200) })
	policy.Register("POST", "/api/v1/datasource", APIKeyRoutePolicy{RequireFullAccess: true})
	engine.POST("/api/v1/datasource", RequireRole(types.TenantRoleAdmin, cfg), func(c *gin.Context) { c.Status(201) })
	engine.GET("/api/v1/datasource", RequireRole(types.TenantRoleViewer, cfg), func(c *gin.Context) { c.Status(200) })
	digest := func(v string) string { d := sha256.Sum256([]byte(v)); return hex.EncodeToString(d[:]) }
	claims := func() jwt.MapClaims {
		return jwt.MapClaims{"iss": "agentos-console", "aud": "weknora-console", "sub": "employee-1", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(), "jti": "unique-request-identifier-12345", "method": "POST", "target": "/api/v1/knowledge-bases", "digest": digest(`{"name":"Test"}`), "key_digest": digest("user-api-key")}
	}
	call := func(c jwt.MapClaims, path, body string) int {
		signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, c).SignedString(private)
		require.NoError(t, err)
		req := httptest.NewRequest(c["method"].(string), path, strings.NewReader(body))
		req.Header.Set("X-AgentOS-Console", signed)
		req.Header.Set("X-API-Key", "user-api-key")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w.Code
	}
	t.Run("the same external API key alone still cannot create a library", func(t *testing.T) {
		request := httptest.NewRequest("POST", "/api/v1/knowledge-bases", strings.NewReader(`{"name":"Denied"}`))
		request.Header.Set("X-API-Key", "user-api-key")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, 403, response.Code)
	})
	t.Run("only the storage default KV is delegated", func(t *testing.T) {
		for _, key := range []string{"storage-engine-config", "parser-engine-config", "other-secret"} {
			c := claims()
			c["method"] = "GET"
			c["target"] = "/api/v1/tenants/kv/" + key
			c["jti"] = "editor-kv-unique-request-" + key
			expected := 403
			if key == "storage-engine-config" {
				expected = 200
			}
			require.Equal(t, expected, call(c, c["target"].(string), `{"name":"Test"}`))
		}
	})
	t.Run("editor capabilities retain real role guards", func(t *testing.T) {
		c := claims()
		c["method"] = "GET"
		c["target"] = "/api/v1/system/parser-engines"
		c["jti"] = "editor-parser-unique-request-12345"
		require.Equal(t, 200, call(c, c["target"].(string), `{"name":"Test"}`))
		c["method"] = "POST"
		c["target"] = "/api/v1/initialization/extract/text-relation"
		c["jti"] = "editor-graph-contributor-request-12345"
		require.Equal(t, 403, call(c, c["target"].(string), `{"name":"Test"}`))
		members.seedActive(userID, 10000, types.TenantRoleAdmin)
		c["jti"] = "editor-graph-admin-request-12345"
		require.Equal(t, 200, call(c, c["target"].(string), `{"name":"Test"}`))
		members.seedActive(userID, 10000, types.TenantRoleContributor)
	})
	t.Run("datasource delegation preserves role requirements and machine key restrictions", func(t *testing.T) {
		c := claims()
		c["target"] = "/api/v1/datasource"
		c["method"] = "GET"
		c["jti"] = "datasource-read-contributor-request"
		require.Equal(t, 200, call(c, c["target"].(string), `{"name":"Test"}`))
		c["method"] = "POST"
		c["jti"] = "datasource-write-contributor-request"
		require.Equal(t, 403, call(c, c["target"].(string), `{"name":"Test"}`))
		members.seedActive(userID, 10000, types.TenantRoleAdmin)
		c["jti"] = "datasource-write-admin-request"
		require.Equal(t, 201, call(c, c["target"].(string), `{"name":"Test"}`))
		request := httptest.NewRequest("POST", "/api/v1/datasource", strings.NewReader(`{"name":"Denied"}`))
		request.Header.Set("X-API-Key", "user-api-key")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, 403, response.Code)
		members.seedActive(userID, 10000, types.TenantRoleContributor)
	})

	t.Run("signature cannot change payload", func(t *testing.T) {
		require.Equal(t, 401, call(claims(), "/api/v1/knowledge-bases", `{"name":"Forged"}`))
	})
	t.Run("signature cannot target administration", func(t *testing.T) {
		require.Equal(t, 403, call(claims(), "/api/v1/system/admin/users", `{"name":"Test"}`))
	})
	t.Run("wrong issuer", func(t *testing.T) {
		c := claims()
		c["iss"] = "other-app"
		require.Equal(t, 401, call(c, "/api/v1/knowledge-bases", `{"name":"Test"}`))
	})
	t.Run("external identity must match key", func(t *testing.T) {
		c := claims()
		c["sub"] = "employee-2"
		require.Equal(t, 401, call(c, "/api/v1/knowledge-bases", `{"name":"Test"}`))
	})
	for _, field := range []string{"aud", "exp", "iat", "method", "target", "key_digest", "content_type"} {
		t.Run("reject modified "+field, func(t *testing.T) {
			c := claims()
			switch field {
			case "exp":
				c[field] = time.Now().Add(-time.Second).Unix()
			case "iat":
				c[field] = time.Now().Add(-2 * time.Minute).Unix()
			case "method":
				c[field] = "PUT"
			default:
				c[field] = "other"
			}
			// Keep the actual request method POST so the signature mismatch is tested.
			signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, c).SignedString(private)
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/api/v1/knowledge-bases", strings.NewReader(`{"name":"Test"}`))
			req.Header.Set("X-AgentOS-Console", signed)
			req.Header.Set("X-API-Key", "user-api-key")
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			require.Equal(t, 401, w.Code)
		})
	}
	t.Run("ownership is enforced for delegated contributor", func(t *testing.T) {
		for _, id := range []string{"mine", "colleague"} {
			c := claims()
			c["method"] = "PUT"
			c["target"] = "/api/v1/knowledge-bases/" + id
			c["jti"] = "ownership-unique-request-" + id
			expected := 403
			if id == "mine" {
				expected = 204
			}
			require.Equal(t, expected, call(c, c["target"].(string), `{"name":"Test"}`))
		}
	})
	t.Run("delegation preserves shared viewer versus editor permissions", func(t *testing.T) {
		c := claims()
		c["method"] = "GET"
		c["target"] = "/api/v1/knowledgebase/shared/wiki/stats"
		c["jti"] = "shared-read-unique-request-12345"
		require.Equal(t, 200, call(c, c["target"].(string), `{"name":"Test"}`))
		c["method"] = "POST"
		c["target"] = "/api/v1/knowledgebase/shared/wiki/auto-fix"
		c["jti"] = "shared-write-denied-request-12345"
		require.Equal(t, 403, call(c, c["target"].(string), `{"name":"Test"}`))
		sharedGrant.permission["shared"] = types.OrgRoleEditor
		c["jti"] = "shared-write-allowed-request-12345"
		require.Equal(t, 201, call(c, c["target"].(string), `{"name":"Test"}`))
	})
	t.Run("live user and replay rejection", func(t *testing.T) {
		require.Equal(t, 201, call(claims(), "/api/v1/knowledge-bases", `{"name":"Test"}`))
		require.Equal(t, 401, call(claims(), "/api/v1/knowledge-bases", `{"name":"Test"}`))
	})
	t.Run("demotion changes authorization", func(t *testing.T) {
		members.seedActive(userID, 10000, types.TenantRoleViewer)
		c := claims()
		c["jti"] = "demoted-request-identifier-12345"
		require.Equal(t, 403, call(c, "/api/v1/knowledge-bases", `{"name":"Test"}`))
	})
	t.Run("disabled account", func(t *testing.T) {
		users.user.IsActive = false
		require.Equal(t, 403, call(claims(), "/api/v1/knowledge-bases", `{"name":"Test"}`))
		users.user.IsActive = true
	})
	t.Run("revoked membership", func(t *testing.T) {
		members.members[memberKey(userID, 10000)].Status = types.TenantMemberStatusSuspended
		require.Equal(t, 403, call(claims(), "/api/v1/knowledge-bases", `{"name":"Test"}`))
	})
	t.Run("nonce storage failure fails closed", func(t *testing.T) {
		members.seedActive(userID, 10000, types.TenantRoleContributor)
		require.NoError(t, cache.Close())
		c := claims()
		c["jti"] = "redis-unavailable-request-12345"
		require.Equal(t, 503, call(c, "/api/v1/knowledge-bases", `{"name":"Test"}`))
	})
	require.False(t, keys.key.FullAccess)
	require.True(t, users.user.Preferences.MustChangePassword)
}

func TestAgentOSConsoleDisabledDoesNotAffectExistingRequests(t *testing.T) {
	t.Setenv("AGENTOS_CONSOLE_PUBLIC_KEY", "")
	engine := gin.New()
	engine.Use(AgentOSConsoleAuth(nil, nil, nil, nil, nil, nil))
	engine.GET("/existing", func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest("GET", "/existing", nil))
	require.Equal(t, 200, w.Code)
	req := httptest.NewRequest("GET", "/existing", bytes.NewReader(nil))
	req.Header.Set("X-AgentOS-Console", "forged")
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, 503, w.Code)
	require.False(t, consoleRouteAllowed(http.MethodPost, "/api/v1/external-users"))
	require.False(t, consoleRouteAllowed(http.MethodPost, "/api/v1/models"))
	require.True(t, consoleRouteAllowed(http.MethodPut, "/api/v1/knowledgebase/:kb_id/wiki/pages/*slug"))
}
