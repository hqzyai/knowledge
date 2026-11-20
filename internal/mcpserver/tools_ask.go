package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/agentturn"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	askSessionTitleMaxLen = 60
	askChannel            = "mcp"
)

func askTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolAsk,
		mcp.WithDescription("Ask the workspace a question and get a synthesized answer with citations. "+
			"WeKnora retrieves from the knowledge bases in scope and runs the agent configured on this "+
			"endpoint. Pass the returned session_id on follow-up questions to keep the conversation going. "+
			"This can take up to a few minutes for agentic runs; prefer search_knowledge when you only need "+
			"raw passages."),
		mcp.WithString("question", mcp.Required(), mcp.Description("The question to answer")),
		mcp.WithString("session_id",
			mcp.Description("Session id returned by a previous ask call, to continue that conversation")),
		mcp.WithArray("knowledge_base_ids", mcp.WithStringItems(),
			mcp.Description("Optional knowledge base ids or names to retrieve from; defaults to the "+
				"endpoint scope or the agent's own configuration")),
		// Not read-only: it creates a session and messages, and the configured
		// agent may run its own tools. Clients must not auto-approve it.
		mcp.WithReadOnlyHintAnnotation(false),
	)
}

type askReference struct {
	KnowledgeID    string     `json:"knowledge_id"`
	KnowledgeTitle string     `json:"knowledge_title,omitempty"`
	ChunkID        string     `json:"chunk_id"`
	Score          float64    `json:"score,omitempty"`
	Excerpt        string     `json:"excerpt"`
	Images         []askImage `json:"images,omitempty"`
}

// Keep image references separate from the truncated excerpt. OriginalURL is
// parser provenance, not a serving URL, and must not be exposed as a fallback.
type askImage struct {
	URL     string `json:"url"`
	Caption string `json:"caption,omitempty"`
	OCRText string `json:"ocr_text,omitempty"`
}

func (s *Server) handleAsk(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ep, err := endpointFromContext(ctx)
	if err != nil {
		return mcp.NewToolResultError("unauthorized"), nil
	}
	question, err := req.RequireString("question")
	if err != nil || strings.TrimSpace(question) == "" {
		return mcp.NewToolResultError("question is required"), nil
	}
	question = strings.TrimSpace(question)

	agent, err := s.resolveAskAgent(ctx, ep)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kbIDs, err := s.resolveAskKnowledgeBases(ctx, ep, agent, req.GetStringSlice("knowledge_base_ids", nil))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	session, err := s.resolveAskSession(ctx, ep, req.GetString("session_id", ""), question)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	answer, rawRefs, err := agentturn.Run(ctx, s.sessionService, s.messageService, &types.QARequest{
		Session: session, CustomAgent: agent, Query: question, KnowledgeBaseIDs: kbIDs,
	}, askChannel)
	refs := summarizeReferences(rawRefs)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("failed to answer", err), nil
	}
	structured := map[string]any{
		"answer":     answer,
		"session_id": session.ID,
		"agent_id":   agent.ID,
		"references": refs,
	}
	var text string
	if len(refs) > 0 {
		var b strings.Builder
		b.WriteString(answer)
		b.WriteString("\n\nSources:\n")
		for i, r := range refs {
			title := r.KnowledgeTitle
			if title == "" {
				title = r.KnowledgeID
			}
			fmt.Fprintf(&b, "[%d] %s (document %s)\n", i+1, title, r.KnowledgeID)
			for _, img := range r.Images {
				// Preserve image references for text-only MCP hosts as well.
				// strconv.Quote, unlike json.Marshal, keeps & < > literal.
				fmt.Fprintf(&b, "  Image: %s\n", strconv.Quote(img.URL))
			}
		}
		fmt.Fprintf(&b, "\nsession_id: %s", session.ID)
		text = b.String()
	} else {
		text = fmt.Sprintf("%s\n\nsession_id: %s", answer, session.ID)
	}
	return mcp.NewToolResultStructured(structured, text), nil
}

// resolveAskAgent picks the agent configured on the endpoint. Callers cannot
// name an agent: letting a client choose any tenant agent (or an internal
// builtin such as the wiki fixer or skill installer, which carry write and
// shell tools) would bypass the endpoint's tool allowlist.
func (s *Server) resolveAskAgent(ctx context.Context, ep *types.MCPEndpoint) (*types.CustomAgent, error) {
	agentID := strings.TrimSpace(ep.DefaultAgentID)
	if agentID == "" {
		agentID = types.BuiltinQuickAnswerID
	}
	agent, err := s.agentService.GetAgentByID(ctx, agentID)
	if err != nil || agent == nil {
		return nil, fmt.Errorf("the agent configured on this endpoint (%q) is not available", agentID)
	}
	if !types.MCPEndpointAgentAllowed(agent, ep.TenantID) {
		return nil, fmt.Errorf("the agent configured on this endpoint (%q) is not available", agentID)
	}
	return agent, nil
}

// resolveAskKnowledgeBases picks the retrieval scope for one ask call:
// explicit selectors win; otherwise a restricted endpoint pins its own list;
// otherwise the agent's configuration decides, except that an agent with no
// knowledge bases of its own falls back to everything the endpoint can see
// so the call does not fail with "no search targets".
func (s *Server) resolveAskKnowledgeBases(
	ctx context.Context, ep *types.MCPEndpoint, agent *types.CustomAgent, requested []string,
) ([]string, error) {
	hasRequest := false
	for _, r := range requested {
		if strings.TrimSpace(r) != "" {
			hasRequest = true
			break
		}
	}
	if hasRequest {
		kbs, err := s.selectKnowledgeBases(ctx, ep, requested)
		if err != nil {
			return nil, err
		}
		return knowledgeBaseIDs(kbs), nil
	}
	if ep.RestrictsKnowledgeBases() {
		kbs, err := s.allowedKnowledgeBases(ctx, ep)
		if err != nil {
			return nil, err
		}
		return knowledgeBaseIDs(kbs), nil
	}
	mode := strings.ToLower(strings.TrimSpace(agent.Config.KBSelectionMode))
	agentHasNoKBs := mode == "none" || ((mode == "selected" || mode == "") && len(agent.Config.KnowledgeBases) == 0)
	if agentHasNoKBs {
		kbs, err := s.allowedKnowledgeBases(ctx, ep)
		if err != nil {
			return nil, err
		}
		return knowledgeBaseIDs(kbs), nil
	}
	return nil, nil
}

func (s *Server) resolveAskSession(
	ctx context.Context, ep *types.MCPEndpoint, sessionID, question string,
) (*types.Session, error) {
	ownerID := types.MCPEndpointPrincipal(ep.TenantID, ep.ID).StorageID()
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		session, err := s.sessionService.GetSessionByID(ctx, ep.TenantID, sessionID)
		if err != nil || session == nil || session.UserID != ownerID {
			return nil, fmt.Errorf("session %q was not found on this endpoint", sessionID)
		}
		return session, nil
	}
	title := question
	if runes := []rune(title); len(runes) > askSessionTitleMaxLen {
		title = string(runes[:askSessionTitleMaxLen])
	}
	created, err := s.sessionService.CreateSession(ctx, &types.Session{
		TenantID:    ep.TenantID,
		Title:       title,
		Description: "MCP endpoint " + ep.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	if err := s.sessionService.SetSessionOwnerID(ctx, ep.TenantID, created.ID, ownerID); err != nil {
		return nil, fmt.Errorf("failed to bind session: %w", err)
	}
	created.UserID = ownerID
	return created, nil
}

const askExcerptMaxRunes = 300

func summarizeReferences(refs []*types.SearchResult) []askReference {
	out := make([]askReference, 0, len(refs))
	seen := map[string]int{}
	for _, r := range refs {
		if r == nil {
			continue
		}
		key := r.ID
		if key == "" {
			key = r.KnowledgeID + ":" + fmt.Sprint(r.ChunkIndex)
		}
		if index, dup := seen[key]; dup {
			out[index].Images = summarizeReferenceImages(r.ImageInfo, out[index].Images)
			continue
		}
		seen[key] = len(out)
		out = append(out, askReference{
			KnowledgeID:    r.KnowledgeID,
			KnowledgeTitle: r.KnowledgeTitle,
			ChunkID:        r.ID,
			Score:          r.Score,
			Excerpt:        truncateExcerpt(strings.TrimSpace(r.Content)),
			Images:         summarizeReferenceImages(r.ImageInfo, nil),
		})
	}
	return out
}

// truncateExcerpt cuts at askExcerptMaxRunes but never inside a storage
// reference: half a handle is unusable and would only cost a failed lookup.
// Whole image references are kept in askReference.Images instead.
func truncateExcerpt(excerpt string) string {
	runes := []rune(excerpt)
	if len(runes) <= askExcerptMaxRunes {
		return excerpt
	}
	cut := len(string(runes[:askExcerptMaxRunes]))
	for _, loc := range types.StorageReferencePattern.FindAllStringIndex(excerpt, -1) {
		if loc[0] >= cut {
			break
		}
		if cut < loc[1] {
			cut = loc[0]
			break
		}
	}
	return excerpt[:cut] + "…"
}

func summarizeReferenceImages(raw string, out []askImage) []askImage {
	var infos []types.ImageInfo
	if err := json.Unmarshal([]byte(raw), &infos); err != nil {
		return out
	}
	seen := make(map[string]int, len(out)+len(infos))
	for i, img := range out {
		seen[img.URL] = i
	}
	for _, info := range infos {
		url := strings.TrimSpace(info.URL)
		if url == "" {
			continue
		}
		if index, dup := seen[url]; dup {
			if out[index].Caption == "" {
				out[index].Caption = info.Caption
			}
			if out[index].OCRText == "" {
				out[index].OCRText = info.OCRText
			}
			continue
		}
		seen[url] = len(out)
		out = append(out, askImage{URL: url, Caption: info.Caption, OCRText: info.OCRText})
	}
	return out
}
