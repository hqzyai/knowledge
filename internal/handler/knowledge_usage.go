package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type KnowledgeUsageHandler struct {
	repo *repository.KnowledgeUsageRepository
}

func NewKnowledgeUsageHandler(repo *repository.KnowledgeUsageRepository) *KnowledgeUsageHandler {
	return &KnowledgeUsageHandler{repo: repo}
}

func (h *KnowledgeUsageHandler) Read(c *gin.Context) {
	observer, ok := c.Get(middleware.AgentOSObserverContext)
	if !ok {
		c.AbortWithStatusJSON(403, gin.H{"error": "AgentOS read-only observer required"})
		return
	}
	q := repository.KnowledgeUsageQuery{View: c.DefaultQuery("view", "libraries"), KBID: c.Query("kb_id"), ResourceID: c.Query("resource_id"), ResourceType: c.Query("resource_type"), Search: strings.TrimSpace(c.Query("q"))}
	var err error
	q.Page, err = strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || q.Page < 1 || q.Page > 1000000 {
		c.Status(400)
		return
	}
	q.PageSize, err = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || q.PageSize < 1 || q.PageSize > 100 {
		c.Status(400)
		return
	}
	days := c.DefaultQuery("days", "30")
	if days != "all" {
		q.Days, err = strconv.Atoi(days)
		if err != nil || q.Days != 7 && q.Days != 30 && q.Days != 90 {
			c.Status(400)
			return
		}
	}
	if len(q.Search) > 200 || len(q.KBID) > 128 || len(q.ResourceID) > 128 {
		c.Status(400)
		return
	}
	valid := q.View == "libraries" && q.KBID == "" && q.ResourceID == "" && q.ResourceType == ""
	if q.KBID != "" {
		if q.ResourceID == "" {
			valid = q.ResourceType == "" && (q.View == "documents" || q.View == "wikis" || q.View == "users")
		} else {
			valid = (q.ResourceType == "document" || q.ResourceType == "wiki") && (q.View == "users" || q.View == "sources" && q.ResourceType == "wiki")
		}
	}
	if !valid {
		c.Status(400)
		return
	}
	result, err := h.repo.Read(c.Request.Context(), types.ExternalUserDefaultTenantID, q)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(404)
		return
	}
	if err != nil {
		logger.Errorf(c.Request.Context(), "Read knowledge usage: %v", err)
		c.JSON(503, gin.H{"error": "Knowledge usage unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, struct {
		*repository.KnowledgeUsageResult
		Observer interface{} `json:"observer"`
	}{result, observer})
}
