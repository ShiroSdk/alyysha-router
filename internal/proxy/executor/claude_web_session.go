// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Conversation lifecycle for the claude.ai lane: the first turn of a
// connection creates a conversation, later turns reuse it, and the
// conversation is deleted once the turn completes so the account's sidebar
// does not fill up with agent chatter. Endpoints are unverified
// (TODO_OWNER in claude_web.go) and kept behind swappable vars for tests.

var (
	claudeWebNewConversationURL = "https://claude.ai/api/organizations/%s/chat_conversations"
	claudeWebConversationURL    = "https://claude.ai/api/organizations/%s/chat_conversations/%s"
)

type claudeConversationEntry struct {
	id        string
	createdAt time.Time
}

var (
	claudeConversationMu  sync.Mutex
	claudeConversations   = map[string]claudeConversationEntry{}
	claudeConversationTTL = 2 * time.Hour // idle conversations are dropped on next sight
	claudeDeleteQueueMu   sync.Mutex
	claudeDeletePending   []claudeDeleteEntry
	claudeDeleteTimer     *time.Timer
	claudeDeleteDelay     = 15 * time.Second
	claudeDeleteTimeout   = 20 * time.Second
)

type claudeDeleteEntry struct {
	cookie         string
	orgID          string
	conversationID string
}

// claudeOrganizationID reads the organization UUID the operator may have
// stored on the connection. claude.ai's web API is organization-scoped;
// TODO_OWNER: the way an operator finds the org id is documented in the
// README section, and the account scope may change — verify live.
func claudeOrganizationID(req *Request) string {
	if req.ConnData != nil {
		if v, ok := req.ConnData["organizationId"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ensureClaudeWebConversation returns the connection's reusable conversation,
// creating one on first use (or after the idle TTL evicted the old one). An
// empty id means the turn runs without a conversation scope and claude.ai
// manages the thread server-side.
func ensureClaudeWebConversation(ctx context.Context, req *Request, cookie, connectionID string) (string, error) {
	claudeConversationMu.Lock()
	entry, ok := claudeConversations[connectionID]
	if ok && time.Since(entry.createdAt) > claudeConversationTTL {
		delete(claudeConversations, connectionID)
		ok = false
	}
	claudeConversationMu.Unlock()
	if ok {
		return entry.id, nil
	}

	orgID := claudeOrganizationID(req)
	if orgID == "" {
		return "", nil
	}
	conversationID, err := claudeWebCreateConversation(ctx, cookie, orgID)
	if err != nil {
		return "", err
	}
	claudeConversationMu.Lock()
	claudeConversations[connectionID] = claudeConversationEntry{id: conversationID, createdAt: time.Now()}
	claudeConversationMu.Unlock()
	return conversationID, nil
}

func claudeWebCreateConversation(ctx context.Context, cookie, orgID string) (string, error) {
	url := fmt.Sprintf(claudeWebNewConversationURL, orgID)
	resp, err := claudeWebDo(ctx, http.MethodPost, url, map[string]string{
		"Content-Type": "application/json",
		"Cookie":       cookie,
		"Origin":       "https://claude.ai",
		"Referer":      "https://claude.ai/chat",
	}, []byte(`{"name":"Alyysha Router"}`))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", claudeWebStatusFailure(resp.StatusCode, string(body))
	}
	var parsed struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.UUID == "" {
		return "", fmt.Errorf("claude-web: conversation create returned no uuid")
	}
	return parsed.UUID, nil
}

// claudeWebQueueConversationDelete retires a conversation shortly after the
// turn completes — an instant create-then-delete in the same second reads as
// scripted traffic, so the delete rides a short timer.
func claudeWebQueueConversationDelete(req *Request, cookie, conversationID string) {
	if conversationID == "" {
		return
	}
	entry := claudeDeleteEntry{cookie: cookie, orgID: claudeOrganizationID(req), conversationID: conversationID}
	claudeDeleteQueueMu.Lock()
	claudeDeletePending = append(claudeDeletePending, entry)
	if claudeDeleteTimer == nil {
		claudeDeleteTimer = time.AfterFunc(claudeDeleteDelay, func() {
			claudeDeleteFlush(context.Background())
		})
	}
	claudeDeleteQueueMu.Unlock()
}

func claudeDeleteFlush(ctx context.Context) {
	claudeDeleteQueueMu.Lock()
	pending := claudeDeletePending
	claudeDeletePending = nil
	claudeDeleteTimer = nil
	claudeDeleteQueueMu.Unlock()

	for _, entry := range pending {
		callCtx, cancel := context.WithTimeout(ctx, claudeDeleteTimeout)
		url := fmt.Sprintf(claudeWebConversationURL, entry.orgID, entry.conversationID)
		if resp, err := claudeWebDo(callCtx, http.MethodDelete, url, map[string]string{
			"Cookie":  entry.cookie,
			"Origin":  "https://claude.ai",
			"Referer": "https://claude.ai/chat",
		}, nil); err == nil && resp != nil {
			_ = resp.Body.Close()
		}
		cancel()
	}
}

// claudeWebResetSessionState clears conversation and delete-queue state;
// tests use it to run cases in isolation.
func claudeWebResetSessionState() {
	claudeConversationMu.Lock()
	claudeConversations = map[string]claudeConversationEntry{}
	claudeConversationMu.Unlock()
	claudeDeleteQueueMu.Lock()
	claudeDeletePending = nil
	if claudeDeleteTimer != nil {
		claudeDeleteTimer.Stop()
		claudeDeleteTimer = nil
	}
	claudeDeleteQueueMu.Unlock()
}
