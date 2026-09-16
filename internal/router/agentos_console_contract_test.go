package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	sessionhandler "github.com/Tencent/WeKnora/internal/handler/session"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/gin-gonic/gin"
	"strings"
	"testing"
)

func TestAgentOSConsoleRoutesResolveToRegisteredHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	r := engine.Group("/api/v1")
	g := &rbacGuards{}
	RegisterUserFavoriteRoutes(r, &handler.UserResourceFavoriteHandler{}, g)
	RegisterKnowledgeBaseRoutes(r, &handler.KnowledgeBaseHandler{}, g)
	RegisterKnowledgeRoutes(r, &handler.KnowledgeHandler{}, g)
	RegisterKnowledgeBaseActivityRoutes(r, &handler.AuditLogHandler{}, g)
	RegisterKnowledgeTagRoutes(r, &handler.TagHandler{}, g)
	RegisterFAQRoutes(r, &handler.FAQHandler{}, g)
	RegisterChunkRoutes(r, &handler.ChunkHandler{}, g)
	RegisterWikiPageRoutes(r, &handler.WikiPageHandler{}, g)
	RegisterChunkerDebugRoutes(r, g)
	RegisterInitializationRoutes(r, &handler.InitializationHandler{}, g)
	RegisterTenantRoutes(r, &handler.TenantHandler{}, &handler.TenantMemberHandler{}, &handler.TenantInvitationHandler{}, &handler.AuditLogHandler{}, g)
	RegisterSystemRoutes(r, &handler.SystemHandler{}, g)
	RegisterOrganizationRoutes(r, &handler.OrganizationHandler{}, g)
	RegisterModelRoutes(r, &handler.ModelHandler{}, &handler.ModelCredentialsHandler{}, g)
	RegisterVectorStoreRoutes(r, &handler.VectorStoreHandler{}, g)
	RegisterStorageBackendRoutes(r, &handler.StorageBackendHandler{}, g)
	RegisterChatRoutes(r, &sessionhandler.Handler{}, g)
	RegisterDataSourceRoutes(r, &handler.DataSourceHandler{}, &handler.DataSourceCredentialsHandler{}, g)
	serveKBScopedFiles(r, g, nil, nil, nil)
	// Auth registration also exposes public login/OIDC routes; only this already
	// existing session projection is in the bridge's allow-list.
	r.GET("/auth/me", func(c *gin.Context) {})
	actual := map[string]bool{}
	for _, route := range engine.Routes() {
		actual[route.Method+" "+route.Path] = true
	}
	for _, route := range middleware.ConsoleRoutePatterns() {
		for _, method := range strings.Fields(route[0]) {
			if !actual[method+" "+route[1]] {
				t.Errorf("console route does not exist: %s %s", method, route[1])
			}
		}
	}
}
