package middleware

// The console bridge is a separate, request-bound service credential. It does
// not alter API-key capabilities or the normal password/OIDC authentication.

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

const consoleAuthenticated = "agentos_console_authenticated"
const consoleMaxBody = 128 << 20

type consoleClaims struct {
	jwt.RegisteredClaims
	Method      string `json:"method"`
	ContentType string `json:"content_type"`
	Target      string `json:"target"`
	Digest      string `json:"digest"`
	KeyDigest   string `json:"key_digest"`
}

// AgentOSConsoleAuth only handles signed console requests. Requests without
// the dedicated header continue through the unchanged normal Auth middleware.
func AgentOSConsoleAuth(tenants interfaces.TenantService, users interfaces.UserService,
	members interfaces.TenantMemberService, keys interfaces.TenantAPIKeyService,
	nonces *redis.Client, cfg *config.Config) gin.HandlerFunc {
	var publicKey *rsa.PublicKey
	if raw := strings.TrimSpace(os.Getenv("AGENTOS_CONSOLE_PUBLIC_KEY")); raw != "" {
		if pem, err := base64.StdEncoding.DecodeString(raw); err == nil {
			publicKey, _ = jwt.ParseRSAPublicKeyFromPEM(pem)
		}
	}
	return func(c *gin.Context) {
		assertion := c.GetHeader("X-AgentOS-Console")
		if assertion == "" {
			c.Next()
			return
		}
		reject := func(code int, message string) { c.AbortWithStatusJSON(code, gin.H{"error": message}) }
		if publicKey == nil || publicKey.N.BitLen() < 2048 || nonces == nil || keys == nil || users == nil || members == nil || tenants == nil || cfg == nil || !cfg.Tenant.IsRBACEnforced() {
			reject(503, "AgentOS console bridge is not configured")
			return
		}
		if len(assertion) > 8192 || !consoleRouteAllowed(c.Request.Method, c.FullPath()) || c.GetHeader("X-Tenant-ID") != "" || c.GetHeader("Authorization") != "" || (c.FullPath() == "/api/v1/tenants/kv/:key" && c.Param("key") != "storage-engine-config") {
			reject(403, "Console operation is not allowed")
			return
		}
		claims := &consoleClaims{}
		token, err := jwt.ParseWithClaims(assertion, claims, func(_ *jwt.Token) (interface{}, error) { return publicKey, nil },
			jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("agentos-console"), jwt.WithAudience("weknora-console"),
			jwt.WithExpirationRequired(), jwt.WithIssuedAt())
		now := time.Now()
		if err != nil || !token.Valid || claims.IssuedAt == nil || claims.ExpiresAt == nil || claims.ExpiresAt.Sub(claims.IssuedAt.Time) > time.Minute || claims.IssuedAt.Before(now.Add(-time.Minute)) || len(claims.ID) < 20 || len(claims.ID) > 100 || claims.Subject == "" || len(claims.Subject) > 128 {
			reject(401, "Invalid console assertion")
			return
		}
		apiKey := c.GetHeader("X-API-Key")
		digest := func(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
		if claims.ContentType != c.GetHeader("Content-Type") || claims.Method != c.Request.Method || claims.Target != c.Request.URL.RequestURI() || claims.KeyDigest != digest([]byte(apiKey)) {
			reject(401, "Console request binding mismatch")
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, consoleMaxBody+1))
		if err != nil || len(body) > consoleMaxBody {
			reject(413, "Console body exceeds limit")
			return
		}
		if claims.Digest != digest(body) {
			reject(401, "Console body binding mismatch")
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		ctx := c.Request.Context()
		key, err := keys.AuthenticateAPIKey(ctx, apiKey)
		tenantID := types.ExternalUserDefaultTenantID
		userID := types.ExternalUserInternalID(tenantID, claims.Subject)
		if err != nil || key == nil || key.IsPlatform() || key.FullAccess || key.TenantIDValue() != tenantID || key.Name != types.ExternalUserAPIKeyNamePrefix+userID {
			reject(401, "Console identity binding mismatch")
			return
		}
		user, err := users.GetUserByID(ctx, userID)
		if err != nil || user == nil || !user.IsActive || user.IsSystemAdmin || user.CanAccessAllTenants {
			reject(403, "Console user is unavailable")
			return
		}
		tenant, err := tenants.GetTenantByID(ctx, tenantID)
		if err != nil || tenant == nil || tenant.Status != "active" {
			reject(403, "Console workspace is unavailable")
			return
		}
		member, err := members.GetMembership(ctx, userID, tenantID)
		if err != nil || member == nil || member.Status != types.TenantMemberStatusActive || !validConsoleRole(member.Role) {
			reject(403, "Console workspace membership is unavailable")
			return
		}
		consumed, err := nonces.SetNX(ctx, "agentos-console:nonce:"+claims.ID, "1", 2*time.Minute).Result()
		if err != nil {
			reject(503, "Console replay protection is unavailable")
			return
		}
		if !consumed {
			reject(401, "Console assertion already used")
			return
		}
		// This is delegated user authentication, never API-key authorization.
		// Password-change restrictions still apply to password login; this channel
		// authenticates a live AgentOS session with its independent service key.
		applyAuthSession(c, authSession{User: user, Principal: types.Principal{Type: types.PrincipalWebUser, ID: user.ID},
			TenantID: tenantID, Tenant: tenant, Role: member.Role})
		c.Set(consoleAuthenticated, true)
		c.Next()
	}
}

func validConsoleRole(role types.TenantRole) bool {
	return role == types.TenantRoleOwner || role == types.TenantRoleAdmin || role == types.TenantRoleContributor || role == types.TenantRoleViewer
}

// Match registered route templates, not prefixes: future admin endpoints are
// denied unless deliberately added. The existing handlers still enforce RBAC.
func consoleRouteAllowed(method, path string) bool {
	for _, rule := range consoleRoutes {
		if strings.Contains(" "+rule[0]+" ", " "+method+" ") && rule[1] == path {
			return true
		}
	}
	return false
}

var consoleRoutes = buildConsoleRoutes()

func buildConsoleRoutes() [][2]string {
	routes := [][2]string{}
	add := func(methods, path string) { routes = append(routes, [2]string{methods, "/api/v1/" + path}) }
	add("GET", "auth/me")
	// Creation-time metadata only; the concrete KV key is checked above.
	add("GET", "tenants/kv/:key")
	for _, name := range []string{"info", "parser-engines", "storage-engine-status"} {
		add("GET", "system/"+name)
	}
	for _, name := range []string{"fabri-tag", "fabri-text", "text-relation"} {
		add("POST", "initialization/extract/"+name)
	}
	add("GET POST", "user/favorites")
	add("DELETE", "user/favorites/:type/:id")
	add("PUT", "initialization/config/:kbId")
	add("GET", "knowledge-bases/:id/files")
	add("GET POST", "knowledge-bases")
	add("POST", "knowledge-bases/copy")
	add("GET", "knowledge-bases/copy/progress/:task_id")
	add("GET PUT DELETE", "knowledge-bases/:id")
	for _, name := range []string{"pin", "visibility"} {
		add("PUT", "knowledge-bases/:id/"+name)
	}
	for _, name := range []string{"duplicate", "hybrid-search"} {
		add("POST", "knowledge-bases/:id/"+name)
	}
	for _, name := range []string{"move-targets", "activity"} {
		add("GET", "knowledge-bases/:id/"+name)
	}
	add("GET DELETE", "knowledge-bases/:id/knowledge")
	add("GET PUT", "knowledge-bases/:id/knowledge/folders")
	for _, name := range []string{"file", "url", "manual"} {
		add("POST", "knowledge-bases/:id/knowledge/"+name)
	}
	for _, name := range []string{"tags", "shares"} {
		add("GET POST", "knowledge-bases/:id/"+name)
	}
	add("PUT DELETE", "knowledge-bases/:id/tags/:tag_id")
	add("PUT DELETE", "knowledge-bases/:id/shares/:share_id")
	add("GET POST DELETE", "knowledge-bases/:id/faq/entries")
	add("GET PUT", "knowledge-bases/:id/faq/entries/:entry_id")
	add("GET", "knowledge-bases/:id/faq/entries/export")
	add("PUT", "knowledge-bases/:id/faq/entries/fields")
	add("PUT", "knowledge-bases/:id/faq/entries/tags")
	add("POST", "knowledge-bases/:id/faq/entries/:entry_id/similar-questions")
	add("POST", "knowledge-bases/:id/faq/entry")
	add("POST", "knowledge-bases/:id/faq/search")
	add("PUT", "knowledge-bases/:id/faq/import/last-result/display")
	add("GET", "faq/import/progress/:task_id")
	add("GET", "knowledge/batch")
	add("GET", "knowledge/search")
	add("PUT", "knowledge/tags")
	for _, name := range []string{"batch-delete", "batch-reparse", "folder", "move"} {
		add("POST", "knowledge/"+name)
	}
	add("GET", "knowledge/move/progress/:task_id")
	add("GET PUT DELETE", "knowledge/:id")
	add("PUT", "knowledge/manual/:id")
	add("PUT", "knowledge/image/:id/:chunk_id")
	for _, name := range []string{"stages", "spans", "download", "preview"} {
		add("GET", "knowledge/:id/"+name)
	}
	for _, name := range []string{"reparse", "cancel-parse", "regenerate-summary"} {
		add("POST", "knowledge/:id/"+name)
	}
	add("GET DELETE", "chunks/:knowledge_id")
	add("GET", "chunks/by-id/:id")
	add("PUT DELETE", "chunks/:knowledge_id/:id")
	add("GET", "chunks/:knowledge_id/:id/revisions")
	add("POST", "chunks/:knowledge_id/:id/revert")
	add("PUT DELETE", "chunks/by-id/:id/questions")
	add("POST", "chunks/by-id/:id/questions/regenerate")
	for _, name := range []string{"pages", "folders"} {
		add("GET POST", "knowledgebase/:kb_id/wiki/"+name)
	}
	add("GET PUT DELETE", "knowledgebase/:kb_id/wiki/pages/*slug")
	add("GET", "knowledgebase/:kb_id/wiki/revisions/*slug")
	add("PUT DELETE", "knowledgebase/:kb_id/wiki/folders/:folder_id")
	add("PUT", "knowledgebase/:kb_id/wiki/move-page")
	add("PUT", "knowledgebase/:kb_id/wiki/issues/:issue_id/status")
	for _, name := range []string{"revert", "rebuild-links", "auto-fix"} {
		add("POST", "knowledgebase/:kb_id/wiki/"+name)
	}
	for _, name := range []string{"index", "graph", "stats", "search", "lint", "issues"} {
		add("GET", "knowledgebase/:kb_id/wiki/"+name)
	}
	for _, name := range []string{"organizations", "shared-knowledge-bases", "models", "vector-stores", "storage-backends"} {
		add("GET", name)
	}
	add("GET", "organizations/:id/shared-knowledge-bases")
	add("POST", "knowledge-search")
	add("POST", "chunker/preview")
	// Knowledge settings use the caller's existing workspace RBAC. This does
	// not grant these capabilities to the API key outside the signed bridge.
	add("GET POST", "datasource")
	add("GET", "datasource/types")
	add("POST", "datasource/validate-credentials")
	add("GET PUT DELETE", "datasource/:id")
	add("PUT", "datasource/:id/credentials")
	add("DELETE", "datasource/:id/credentials/:field")
	for _, name := range []string{"resources", "logs"} {
		add("GET", "datasource/:id/"+name)
	}
	add("GET", "datasource/logs/:log_id")
	for _, name := range []string{"validate", "resource-ancestors", "sync", "pause", "resume"} {
		add("POST", "datasource/:id/"+name)
	}
	return routes
}

// ConsoleRoutePatterns exposes the small public bridge contract for parity
// tests and the BFF integration docs, without exposing service configuration.
func ConsoleRoutePatterns() [][2]string { return append([][2]string(nil), consoleRoutes...) }
