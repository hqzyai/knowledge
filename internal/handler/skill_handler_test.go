package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

type fakeUsableSkillLister struct {
	tenantID uint64
	configID string
	skills   []*types.TenantSkillEntity
}

func (f *fakeUsableSkillLister) ListUsableSkills(
	_ context.Context, tenantID uint64, configID string,
) []*types.TenantSkillEntity {
	f.tenantID = tenantID
	f.configID = configID
	if configID == "" {
		return nil
	}
	return f.skills
}

func newChatSkillRouter(h *SkillHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), testSkillTenantID)
		c.Next()
	})
	r.GET("/skills", h.ListSkills)
	return r
}

func TestListSkillsHidesThePickerWhenNoSandboxConfigIsSelected(t *testing.T) {
	lister := &fakeUsableSkillLister{
		skills: []*types.TenantSkillEntity{{Name: "ppt-generator", Description: "make ppt"}},
	}
	router := newChatSkillRouter(NewSkillHandler(lister, nil))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/skills", nil))

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Success         bool `json:"success"`
		Data            []SkillInfoResponse
		SkillsAvailable bool `json:"skills_available"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.Empty(t, body.Data, "preloaded and unscoped skills must not appear in @")
	require.False(t, body.SkillsAvailable)
	require.Empty(t, lister.configID)
}

func TestListSkillsReturnsUsableInstalledSkillsForTheSelectedConfig(t *testing.T) {
	lister := &fakeUsableSkillLister{
		skills: []*types.TenantSkillEntity{
			{Name: "ppt-generator", Description: "make ppt"},
		},
	}
	router := newChatSkillRouter(NewSkillHandler(lister, nil))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(
		http.MethodGet, "/skills?sandbox_config_id=cfg-1", nil,
	))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, testSkillTenantID, lister.tenantID)
	require.Equal(t, "cfg-1", lister.configID)

	var body struct {
		Success         bool `json:"success"`
		Data            []SkillInfoResponse
		SkillsAvailable bool `json:"skills_available"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.True(t, body.SkillsAvailable)
	require.Equal(t, []SkillInfoResponse{
		{Name: "ppt-generator", Description: "make ppt"},
	}, body.Data)
}

func TestDownloadWeKnoraSkillEmbedsRequestedBaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewSkillHandler(nil, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	requested := "https://kb.example.com/weknora/api/v1/"
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/skills/weknora/download?base_url="+url.QueryEscape(requested), nil)

	handler.DownloadWeKnoraSkill(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, "weknora-skill.zip") {
		t.Fatalf("Content-Disposition = %q", got)
	}

	skill := readZipFile(t, recorder.Body.Bytes(), "weknora/SKILL.md")
	if !strings.Contains(skill, "'https://kb.example.com/weknora/api/v1'") {
		t.Fatalf("generated SKILL.md does not contain normalized base URL:\n%s", skill)
	}
	if !strings.Contains(skill, `"env": ["WEKNORA_API_KEY"]`) {
		t.Fatalf("generated SKILL.md does not require WEKNORA_API_KEY")
	}
	if strings.Contains(skill, `"env": ["WEKNORA_API_KEY", "WEKNORA_BASE_URL"]`) {
		t.Fatalf("generated SKILL.md still requires WEKNORA_BASE_URL")
	}
	_ = readZipFile(t, recorder.Body.Bytes(), "weknora/skill-card.md")
}

func TestDownloadWeKnoraSkillUsesContainerServiceDefault(t *testing.T) {
	archive, err := buildWeKnoraSkillArchive(defaultWeKnoraSkillBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	skill := readZipFile(t, archive, "weknora/SKILL.md")
	if !strings.Contains(skill, "'http://app:8080/api/v1'") {
		t.Fatalf("generated SKILL.md does not contain container-service default")
	}
}

func TestNormalizeWeKnoraSkillBaseURLRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{
		"javascript:alert(1)",
		"https://user:secret@example.com/api/v1",
		"https://example.com/api/v2",
		"https://example.com/api/v1?tenant=one",
		"https://example.com/api/v1#fragment",
		"https://example.com/api/v1\nmalicious",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := normalizeWeKnoraSkillBaseURL(input); err == nil {
				t.Fatalf("normalizeWeKnoraSkillBaseURL(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func readZipFile(t *testing.T, data []byte, name string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		return string(content)
	}
	t.Fatalf("zip entry %q not found", name)
	return ""
}
