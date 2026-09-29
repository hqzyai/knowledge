package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAgentOSObserverIsBoundReadOnlyAndReplayProtected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	require.NoError(t, err)
	t.Setenv("AGENTOS_CONSOLE_PUBLIC_KEY", base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})))
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	enabled := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
	users := &consoleUsers{user: &types.User{ID: "employee", IsActive: true}}
	engine := gin.New()
	engine.Use(AgentOSConsoleAuth(&consoleTenants{}, users, newFakeMemberService(), &consoleKeys{}, cache, cfg))
	engine.Use(Auth(&consoleTenants{}, users, newFakeMemberService(), &consoleKeys{}, cfg))
	engine.GET("/api/v1/knowledge-analytics", func(c *gin.Context) {
		binding, ok := c.Get(AgentOSObserverContext)
		require.True(t, ok)
		require.Equal(t, "viewer", binding.(gin.H)["role"])
		require.Equal(t, "builtin-admin", binding.(gin.H)["external_id"])
		_, hasUser := types.UserIDFromContext(c.Request.Context())
		require.False(t, hasUser)
		c.Status(200)
	})
	engine.POST("/api/v1/knowledge-bases", func(c *gin.Context) { t.Fatal("observer reached write handler") })
	engine.GET("/api/v1/knowledge-bases", func(c *gin.Context) { t.Fatal("observer reached employee handler") })
	digest := sha256.Sum256(nil)
	claims := func() jwt.MapClaims {
		return jwt.MapClaims{"iss": "agentos-console", "aud": "weknora-console", "sub": "builtin-admin", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(), "jti": uuid.NewString(), "method": "GET", "target": "/api/v1/knowledge-analytics", "digest": hex.EncodeToString(digest[:]), "key_digest": hex.EncodeToString(digest[:]), "scope": "knowledge-analytics:read", "role": "system_admin"}
	}
	call := func(claim jwt.MapClaims, method, path, key string) int {
		token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claim).SignedString(private)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-AgentOS-Console", token)
		req.Header.Set("X-API-Key", key)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response.Code
	}
	c := claims()
	require.Equal(t, 200, call(c, "GET", "/api/v1/knowledge-analytics", ""))
	require.Equal(t, 401, call(c, "GET", "/api/v1/knowledge-analytics", ""))
	for _, path := range []string{"/api/v1/knowledge-bases", "/api/v1/knowledge-analytics"} {
		c = claims()
		c["method"], c["target"] = "POST", path
		require.Equal(t, 403, call(c, "POST", path, ""))
	}
	c = claims()
	c["target"] = "/api/v1/knowledge-bases"
	require.Equal(t, 403, call(c, "GET", "/api/v1/knowledge-bases", ""))
	c = claims()
	c["role"] = "member"
	require.Equal(t, 403, call(c, "GET", "/api/v1/knowledge-analytics", ""))
	c = claims()
	require.Equal(t, 401, call(c, "GET", "/api/v1/knowledge-analytics?kb_id=changed", ""))
	c = claims()
	require.Equal(t, 401, call(c, "GET", "/api/v1/knowledge-analytics", "employee-key"))
	cache.Close()
	c = claims()
	require.Equal(t, 503, call(c, "GET", "/api/v1/knowledge-analytics", ""))
}
