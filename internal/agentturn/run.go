// Package agentturn runs a single QA turn for callers that need the completed answer.
package agentturn

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// Run persists the question and answer in the supplied caller-owned session.
// AgentQA completes synchronously, while KnowledgeQA may publish its answer later.
func Run(ctx context.Context, sessions interfaces.SessionService, messages interfaces.MessageService, req *types.QARequest, channel string) (string, []*types.SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	requestID := uuid.New().String()
	userMsg, err := messages.CreateMessage(ctx, &types.Message{
		SessionID:   req.Session.ID,
		Role:        "user",
		Content:     req.Query,
		RequestID:   requestID,
		CreatedAt:   time.Now(),
		IsCompleted: true,
		Channel:     channel,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create user message: %w", err)
	}
	assistantMsg, err := messages.CreateMessage(ctx, &types.Message{
		SessionID:   req.Session.ID,
		Role:        "assistant",
		RequestID:   requestID,
		CreatedAt:   time.Now(),
		IsCompleted: false,
		Channel:     channel,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	var (
		mu       sync.Mutex
		answer   strings.Builder
		final    string
		refs     []*types.SearchResult
		qaErr    error
		done     = make(chan struct{})
		complete = make(chan struct{})
		doneOnce sync.Once
		compOnce sync.Once
	)
	closeDone := func() { doneOnce.Do(func() { close(done) }) }
	closeComplete := func() { compOnce.Do(func() { close(complete) }) }

	bus := event.NewEventBus()
	bus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentFinalAnswerData)
		if !ok {
			return nil
		}
		mu.Lock()
		answer.WriteString(data.Content)
		mu.Unlock()
		if data.Done {
			closeDone()
		}
		return nil
	})
	bus.On(event.EventAgentReferences, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentReferencesData)
		if !ok {
			return nil
		}
		mu.Lock()
		collectReferences(&refs, data.References)
		mu.Unlock()
		return nil
	})
	bus.On(event.EventAgentComplete, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentCompleteData)
		if !ok {
			return nil
		}
		mu.Lock()
		if data.FinalAnswer != "" {
			final = data.FinalAnswer
		}
		if len(data.KnowledgeRefs) > 0 {
			var completeRefs []*types.SearchResult
			collectReferences(&completeRefs, data.KnowledgeRefs)
			if len(completeRefs) > 0 {
				refs = completeRefs
			}
		}
		mu.Unlock()
		closeComplete()
		return nil
	})
	bus.On(event.EventError, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.ErrorData)
		if !ok {
			return nil
		}
		mu.Lock()
		qaErr = fmt.Errorf("%s", data.Error)
		mu.Unlock()
		closeDone()
		closeComplete()
		return nil
	})

	req.AssistantMessageID = assistantMsg.ID
	req.UserMessageID = userMsg.ID
	useAgent := req.CustomAgent.IsAgentMode()
	go func() {
		if useAgent {
			defer closeDone()
			defer closeComplete()
		}
		var runErr error
		if useAgent {
			runErr = sessions.AgentQA(ctx, req, bus)
		} else {
			runErr = sessions.KnowledgeQA(ctx, req, bus)
		}
		if runErr != nil {
			mu.Lock()
			if qaErr == nil {
				qaErr = runErr
			}
			mu.Unlock()
			closeDone()
			closeComplete()
		}
	}()

	select {
	case <-done:
		if useAgent {
			timer := time.NewTimer(15 * time.Second)
			select {
			case <-complete:
			case <-ctx.Done():
			case <-timer.C:
				logger.Warnf(ctx, "[agent turn] ask timed out waiting for agent completion: session=%s", req.Session.ID)
			}
			timer.Stop()
		}
	case <-ctx.Done():
		assistantMsg.Content = "The answer was interrupted before completion."
		assistantMsg.IsCompleted = true
		if updateErr := messages.UpdateMessage(context.WithoutCancel(ctx), assistantMsg); updateErr != nil {
			logger.Warnf(ctx, "[agent turn] failed to finalize interrupted message: %v", updateErr)
		}
		return "", nil, fmt.Errorf("answer timed out: %w", ctx.Err())
	}

	mu.Lock()
	text := answer.String()
	if strings.TrimSpace(text) == "" && final != "" {
		text = final
	}
	collected := append([]*types.SearchResult(nil), refs...)
	runErr := qaErr
	mu.Unlock()

	if strings.TrimSpace(text) == "" && runErr != nil {
		assistantMsg.Content = "The answer failed: " + runErr.Error()
		assistantMsg.IsCompleted = true
		if updateErr := messages.UpdateMessage(context.WithoutCancel(ctx), assistantMsg); updateErr != nil {
			logger.Warnf(ctx, "[agent turn] failed to finalize failed message: %v", updateErr)
		}
		return "", nil, runErr
	}
	if strings.TrimSpace(text) == "" {
		text = "No answer could be produced for this question."
	}
	assistantMsg.Content = text
	assistantMsg.IsCompleted = true
	if len(collected) > 0 {
		assistantMsg.KnowledgeReferences = types.References(collected)
	}
	if err := messages.UpdateMessage(context.WithoutCancel(ctx), assistantMsg); err != nil {
		logger.Warnf(ctx, "[agent turn] failed to persist assistant message: %v", err)
	}
	return text, collected, nil
}

func collectReferences(dst *[]*types.SearchResult, raw interface{}) {
	switch v := raw.(type) {
	case []*types.SearchResult:
		*dst = append(*dst, v...)
	case []interface{}:
		for _, item := range v {
			if sr, ok := item.(*types.SearchResult); ok {
				*dst = append(*dst, sr)
			}
		}
	}
}
