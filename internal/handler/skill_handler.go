package handler

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

const (
	defaultWeKnoraSkillBaseURL = "http://app:8080/api/v1"
	weKnoraSkillBaseURLToken   = "{{WEKNORA_BASE_URL}}"
)

//go:embed weknora-skill/SKILL.md
var weKnoraSkillTemplate string

//go:embed weknora-skill/skill-card.md
var weKnoraSkillCard string

// usableSkillLister returns the installed skills a chat turn can actually
// invoke on one sandbox config. The @ picker and the agent editor both read
// this set so they cannot offer a skill the running image does not carry.
type usableSkillLister interface {
	ListUsableSkills(ctx context.Context, tenantID uint64, configID string) []*types.TenantSkillEntity
}

// SkillHandler handles skill-related HTTP requests
type SkillHandler struct {
	usableSkills usableSkillLister
	catalog      skillCatalogService
}

type skillCatalogService interface {
	ListCatalog(ctx context.Context, tenantID uint64) ([]service.SkillCatalogView, error)
	RegisterCatalogFromArchive(ctx context.Context, tenantID uint64, archive []byte) (*types.TenantSkillCatalogEntity, error)
	RegisterCatalogFromSource(ctx context.Context, tenantID uint64, source string) (*types.TenantSkillCatalogEntity, error)
	InstallCatalogToConfigs(ctx context.Context, tenantID uint64, catalogID string, configIDs []string) (*service.CatalogInstallResult, error)
	DeleteCatalog(ctx context.Context, tenantID uint64, catalogID string) error
	ListCatalogFiles(ctx context.Context, tenantID uint64, catalogID string) ([]service.SkillFileEntry, error)
	ReadCatalogFile(ctx context.Context, tenantID uint64, catalogID, relativePath string) (*service.SkillFileContent, error)
}

// NewSkillHandler creates a new skill handler. catalog may be nil in tests
// that only exercise the chat picker.
func NewSkillHandler(usableSkills usableSkillLister, catalog skillCatalogService) *SkillHandler {
	return &SkillHandler{
		usableSkills: usableSkills,
		catalog:      catalog,
	}
}

// SkillInfoResponse represents the skill info returned to frontend
type SkillInfoResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListSkills godoc
// @Summary      获取当前沙箱配置上可执行的 Skills
// @Description  返回指定沙箱配置镜像内、智能体实际能调用的已安装技能（ready 且启用）。不传 sandbox_config_id 时列表为空。
// @Tags         Skills
// @Accept       json
// @Produce      json
// @Param        sandbox_config_id  query     string  false  "Sandbox config ID"
// @Success      200  {object}  map[string]interface{}  "Skills列表"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /skills [get]
func (h *SkillHandler) ListSkills(c *gin.Context) {
	configID := c.Query("sandbox_config_id")
	if configID == "" || h.usableSkills == nil {
		c.JSON(http.StatusOK, gin.H{
			"success":          true,
			"data":             []SkillInfoResponse{},
			"skills_available": false,
		})
		return
	}

	rows := h.usableSkills.ListUsableSkills(
		c.Request.Context(), sandboxConfigTenantID(c), configID,
	)
	response := make([]SkillInfoResponse, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		response = append(response, SkillInfoResponse{
			Name:        row.Name,
			Description: row.Description,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success":          true,
		"data":             response,
		"skills_available": true,
	})
}

// DownloadWeKnoraSkill godoc
// @Summary      下载 WeKnora Skill
// @Description  下载一个已写入 API Base URL、仅需通过环境变量提供 API Key 的 WeKnora Skill ZIP
// @Tags         Skills
// @Produce      application/zip
// @Param        base_url  query  string  false  "写入 Skill 的 API Base URL（必须以 /api/v1 结尾）"
// @Success      200  {file}  binary  "weknora-skill.zip"
// @Failure      400  {object}  map[string]interface{}  "API Base URL 无效"
// @Failure      500  {object}  map[string]interface{}  "生成 ZIP 失败"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /skills/weknora/download [get]
func (h *SkillHandler) DownloadWeKnoraSkill(c *gin.Context) {
	baseURL, err := normalizeWeKnoraSkillBaseURL(c.Query("base_url"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	archive, err := buildWeKnoraSkillArchive(baseURL)
	if err != nil {
		logger.ErrorWithFields(c.Request.Context(), err, nil)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "failed to build skill archive"})
		return
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": "weknora-skill.zip"})
	c.Header("Content-Disposition", disposition)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/zip", archive)
}

func normalizeWeKnoraSkillBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultWeKnoraSkillBaseURL, nil
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '`' {
			return "", fmt.Errorf("base_url contains unsupported characters")
		}
	}

	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("base_url must be an absolute HTTP or HTTPS URL")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("base_url must not contain credentials")
	}
	if parsed.Fragment != "" {
		return "", fmt.Errorf("base_url must not contain a fragment")
	}
	if parsed.RawQuery != "" {
		return "", fmt.Errorf("base_url must not contain a query string")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	if !strings.HasSuffix(parsed.Path, "/api/v1") {
		return "", fmt.Errorf("base_url must end with /api/v1")
	}
	return parsed.String(), nil
}

func buildWeKnoraSkillArchive(baseURL string) ([]byte, error) {
	skill := strings.ReplaceAll(weKnoraSkillTemplate, weKnoraSkillBaseURLToken, quoteWeKnoraSkillShellValue(baseURL))
	if strings.Contains(skill, weKnoraSkillBaseURLToken) {
		return nil, fmt.Errorf("unresolved skill base URL placeholder")
	}

	files := []struct {
		name    string
		content string
	}{
		{name: "weknora/SKILL.md", content: skill},
		{name: "weknora/skill-card.md", content: weKnoraSkillCard},
	}

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.SetMode(0o644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("create %s: %w", file.name, err)
		}
		if _, err := entry.Write([]byte(file.content)); err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("write %s: %w", file.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close skill archive: %w", err)
	}
	return buffer.Bytes(), nil
}

func quoteWeKnoraSkillShellValue(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
