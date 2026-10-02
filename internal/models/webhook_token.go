package models

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// WebhookTokenStatus defines the lifecycle state of a webhook token.
type WebhookTokenStatus string

const (
	WebhookTokenStatusActive  WebhookTokenStatus = "active"
	WebhookTokenStatusRevoked WebhookTokenStatus = "revoked"
)

// WebhookScope names one thing a webhook token may do. A token holds a set of scopes, and each
// webhook route requires one. Scopes exist so a token minted to post messages does not also read
// every thread its owner has: access is granted when the token is created, not assumed.
type WebhookScope string

const (
	// WebhookScopeMessagesWrite allows POST /webhooks/chat/{chatId}/messages. It is the only
	// thing webhook tokens could do before scopes existed.
	WebhookScopeMessagesWrite WebhookScope = "messages:write"
	// WebhookScopeChatRead allows the read routes: threads, messages, job status and the persona
	// list (names only). It never exposes system prompts, scratchpads or memories.
	WebhookScopeChatRead WebhookScope = "chat:read"
)

// AllWebhookScopes lists every valid scope, in documentation order.
var AllWebhookScopes = []WebhookScope{WebhookScopeMessagesWrite, WebhookScopeChatRead}

// DefaultWebhookScopes is what a token gets when its creator does not choose: the least it needs to
// behave as tokens always have. Read access has to be asked for.
var DefaultWebhookScopes = []WebhookScope{WebhookScopeMessagesWrite}

// LegacyWebhookScopes is what a token created before scopes existed is treated as holding. It is
// deliberately the same as the default: those tokens keep posting and do not start reading.
var LegacyWebhookScopes = DefaultWebhookScopes

// NormalizeWebhookScopes validates a requested scope list: unknown scopes are an error, duplicates
// collapse, and the result is in a stable order. An empty request means the default scopes.
func NormalizeWebhookScopes(requested []string) ([]WebhookScope, error) {
	if len(requested) == 0 {
		return slices.Clone(DefaultWebhookScopes), nil
	}
	want := map[WebhookScope]bool{}
	for _, raw := range requested {
		scope := WebhookScope(strings.TrimSpace(raw))
		if !slices.Contains(AllWebhookScopes, scope) {
			valid := make([]string, len(AllWebhookScopes))
			for i, s := range AllWebhookScopes {
				valid[i] = string(s)
			}
			return nil, fmt.Errorf("unknown scope %q; valid scopes: %s", raw, strings.Join(valid, ", "))
		}
		want[scope] = true
	}
	out := make([]WebhookScope, 0, len(want))
	for _, s := range AllWebhookScopes {
		if want[s] {
			out = append(out, s)
		}
	}
	return out, nil
}

// EffectiveWebhookScopes turns a stored scope list into what the token may do. Nothing stored
// (every token from before scopes) means the legacy, write-only set.
func EffectiveWebhookScopes(stored []string) []WebhookScope {
	if len(stored) == 0 {
		return slices.Clone(LegacyWebhookScopes)
	}
	out := make([]WebhookScope, 0, len(stored))
	for _, s := range stored {
		if scope := WebhookScope(s); slices.Contains(AllWebhookScopes, scope) {
			out = append(out, scope)
		}
	}
	return out
}

// WebhookScopeStrings converts scopes to plain strings for storage.
func WebhookScopeStrings(scopes []WebhookScope) []string {
	out := make([]string, len(scopes))
	for i, s := range scopes {
		out[i] = string(s)
	}
	return out
}

// WebhookToken is a user-scoped API token used for webhook-authenticated routes.
type WebhookToken struct {
	ID         uuid.UUID          `json:"id"`
	UserID     uuid.UUID          `json:"user_id"`
	Name       string             `json:"name"`
	Status     WebhookTokenStatus `json:"status"`
	Scopes     []WebhookScope     `json:"scopes"`
	LastUsedAt *time.Time         `json:"last_used_at,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

// WebhookAuthPrincipal captures webhook authentication identity details.
type WebhookAuthPrincipal struct {
	UserID         uuid.UUID
	Role           string
	Timezone       string
	WebhookTokenID uuid.UUID
	Scopes         []WebhookScope
}

// HasScope reports whether the principal's token holds scope.
func (p *WebhookAuthPrincipal) HasScope(scope WebhookScope) bool {
	return p != nil && slices.Contains(p.Scopes, scope)
}
