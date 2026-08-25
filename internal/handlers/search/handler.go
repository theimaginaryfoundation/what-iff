package search

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const (
	minQueryRunes      = 2
	maxQueryRunes      = 100
	defaultLimitPerTyp = 5
	maxLimitPerType    = 25
	descriptionMaxRune = 80
)

// Handler serves the cross-resource search endpoint backing the command palette.
type Handler struct {
	ds     Store
	logger *zap.Logger
}

// NewHandler builds a Handler with the supplied datastore and logger.
func NewHandler(ds Store, logger *zap.Logger) *Handler {
	return &Handler{ds: ds, logger: logger}
}

// RegisterRoutes wires GET /search onto the authenticated subrouter.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/search", h.Search).Methods(http.MethodGet)
}

// Search aggregates user-owned matches across chats, personalities, rituals,
// memories, and image gallery items into a single sectioned response. Per-
// section errors are logged and returned as empty results so a transient
// failure on one resource does not blank the whole palette.
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	query, types, limitPerType, err := parseSearchParams(r)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return
	}

	resp := SearchResponse{
		Query:    query,
		Sections: make([]SearchSection, 0, len(types)),
	}

	results := h.runSections(r.Context(), userID, query, types, limitPerType)
	for _, t := range types {
		section := SearchSection{Type: t, Results: results[t]}
		if section.Results == nil {
			section.Results = []SearchResult{}
		}
		resp.Sections = append(resp.Sections, section)
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, resp)
}

// runSections executes one query per requested resource type concurrently and
// returns a map of type -> results. Per-section errors are swallowed (after
// logging) so the response is always best-effort, never all-or-nothing.
func (h *Handler) runSections(ctx context.Context, userID uuid.UUID, query string, types []string, limitPerType int) map[string][]SearchResult {
	out := make(map[string][]SearchResult, len(types))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	for _, t := range types {
		t := t
		g.Go(func() error {
			results, err := h.fetchSection(gctx, userID, t, query, limitPerType)
			if err != nil {
				h.logger.Warn("search: section failed; returning empty",
					zap.String("user_id", userID.String()),
					zap.String("section", t),
					zap.Error(err))
				results = nil
			}
			mu.Lock()
			out[t] = results
			mu.Unlock()
			return nil
		})
	}
	// Per-section goroutines never return errors (they swallow + log), so Wait
	// only surfaces context cancellation. Best-effort: ignore.
	_ = g.Wait()
	return out
}

func (h *Handler) fetchSection(ctx context.Context, userID uuid.UUID, sectionType, query string, limit int) ([]SearchResult, error) {
	switch sectionType {
	case TypeChat:
		return h.searchChats(ctx, userID, query, limit)
	case TypePersonality:
		return h.searchPersonalities(ctx, userID, query, limit)
	case TypeRitual:
		return h.searchRituals(ctx, userID, query, limit)
	case TypeMemory:
		return h.searchMemories(ctx, userID, query, limit)
	case TypeImage:
		return h.searchImages(ctx, userID, query, limit)
	default:
		return nil, errors.New("unknown section type: " + sectionType)
	}
}

func (h *Handler) searchChats(ctx context.Context, userID uuid.UUID, query string, limit int) ([]SearchResult, error) {
	page, err := h.ds.ListChats(ctx, userID, 1, limit, models.ChatFilters{Query: &query})
	if err != nil {
		return nil, err
	}

	chats := make([]*models.Chat, 0, len(page.Results))
	chatIDs := make([]uuid.UUID, 0, len(page.Results))
	for _, raw := range page.Results {
		chat, ok := raw.(*models.Chat)
		if !ok || chat == nil {
			continue
		}
		chats = append(chats, chat)
		chatIDs = append(chatIDs, chat.ID)
	}

	// Best-effort snippet enrichment. A failure here should not blank chat hits.
	snippets, snippetErr := h.ds.GetLatestMessagesForChats(ctx, userID, chatIDs)
	if snippetErr != nil {
		h.logger.Warn("search: failed to load chat snippets",
			zap.String("user_id", userID.String()),
			zap.Error(snippetErr))
		snippets = map[uuid.UUID]string{}
	}

	results := make([]SearchResult, 0, len(chats))
	for _, chat := range chats {
		s := scoreFields(query, append([]string{chat.Name}, chat.Tags...)...)
		if s == ScoreNoMatch {
			continue
		}
		results = append(results, SearchResult{
			ID:          chat.ID.String(),
			Label:       chat.Name,
			Description: chatDescription(chat),
			Route:       routeFor(TypeChat, chat.ID),
			IconType:    TypeChat,
			Score:       s,
			Snippet:     trimSnippet(snippets[chat.ID]),
		})
	}
	sortResults(results)
	return results, nil
}

func (h *Handler) searchPersonalities(ctx context.Context, userID uuid.UUID, query string, limit int) ([]SearchResult, error) {
	page, err := h.ds.ListPersonalities(ctx, userID, 1, limit, models.PersonalityFilters{Query: &query})
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(page.Results))
	for _, raw := range page.Results {
		p, ok := raw.(*models.Personality)
		if !ok || p == nil {
			continue
		}
		s := scoreFields(query, p.Name, p.SystemPrompt)
		if s == ScoreNoMatch {
			continue
		}
		results = append(results, SearchResult{
			ID:          p.ID.String(),
			Label:       p.Name,
			Description: trimDescription(p.SystemPrompt),
			Route:       routeFor(TypePersonality, p.ID),
			IconType:    TypePersonality,
			Score:       s,
			Snippet:     trimSnippet(p.SystemPrompt),
		})
	}
	sortResults(results)
	return results, nil
}

func (h *Handler) searchRituals(ctx context.Context, userID uuid.UUID, query string, limit int) ([]SearchResult, error) {
	page, err := h.ds.ListRituals(ctx, userID, 1, limit, models.RitualFilters{Query: &query})
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(page.Results))
	for _, raw := range page.Results {
		rit, ok := raw.(*models.Ritual)
		if !ok || rit == nil {
			continue
		}
		s := scoreFields(query, rit.Name, rit.Description, rit.Content)
		if s == ScoreNoMatch {
			continue
		}
		results = append(results, SearchResult{
			ID:          rit.ID.String(),
			Label:       rit.Name,
			Description: trimDescription(rit.Description),
			Route:       routeFor(TypeRitual, rit.ID),
			IconType:    TypeRitual,
			Score:       s,
			Snippet:     trimSnippet(rit.Content),
		})
	}
	sortResults(results)
	return results, nil
}

func (h *Handler) searchMemories(ctx context.Context, userID uuid.UUID, query string, limit int) ([]SearchResult, error) {
	page, err := h.ds.ListMemories(ctx, userID, 1, limit, models.MemoryFilters{Query: &query})
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(page.Results))
	for _, raw := range page.Results {
		mem, ok := raw.(*models.Memory)
		if !ok || mem == nil {
			continue
		}
		s := score(mem.Content, query)
		if s == ScoreNoMatch {
			continue
		}
		results = append(results, SearchResult{
			ID:          mem.ID.String(),
			Label:       memoryLabel(mem),
			Description: memoryDescription(mem),
			Route:       routeFor(TypeMemory, mem.ID),
			IconType:    TypeMemory,
			Score:       s,
			Snippet:     trimSnippet(mem.Content),
		})
	}
	sortResults(results)
	return results, nil
}

func (h *Handler) searchImages(ctx context.Context, userID uuid.UUID, query string, limit int) ([]SearchResult, error) {
	imageType := models.ImageMIMEPrefix
	page, err := h.ds.ListFileAttachments(ctx, userID, 1, limit, models.FileAttachmentFilters{
		Name:     &query,
		FileType: &imageType,
	})
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(page.Results))
	for _, raw := range page.Results {
		att, ok := raw.(*models.FileAttachment)
		if !ok || att == nil {
			continue
		}
		s := score(att.Name, query)
		if s == ScoreNoMatch {
			continue
		}
		results = append(results, SearchResult{
			ID:          att.ID.String(),
			Label:       att.Name,
			Description: att.FileType,
			Route:       routeFor(TypeImage, att.ID),
			IconType:    TypeImage,
			Score:       s,
		})
	}
	sortResults(results)
	return results, nil
}

// chatDescription returns a short subtitle for a chat row.
func chatDescription(chat *models.Chat) string {
	if chat == nil {
		return "Chat"
	}
	if chat.PersonalityName != "" && chat.ModelName != "" {
		return chat.PersonalityName + " · " + chat.ModelName
	}
	if chat.PersonalityName != "" {
		return chat.PersonalityName
	}
	if chat.ModelName != "" {
		return chat.ModelName
	}
	return "Chat"
}

func memoryLabel(mem *models.Memory) string {
	if mem == nil {
		return ""
	}
	if mem.ChatName != "" {
		return "Memory from " + mem.ChatName
	}
	return scopeLabel(mem.Scope) + " memory"
}

func memoryDescription(mem *models.Memory) string {
	if mem == nil || mem.Scope == "" {
		return ""
	}
	return scopeLabel(mem.Scope) + " memory"
}

// scopeLabel converts memory scope tokens (e.g. "User", "Chat") to a stable
// title-case display form without depending on the deprecated strings.Title.
func scopeLabel(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}
	lower := strings.ToLower(scope)
	return strings.ToUpper(lower[:1]) + lower[1:]
}

// trimDescription clamps long bodies to descriptionMaxRune runes so the
// command palette has a stable subtitle line per result.
func trimDescription(text string) string {
	cleaned := strings.Join(strings.Fields(text), " ")
	if cleaned == "" {
		return ""
	}
	runes := []rune(cleaned)
	if len(runes) <= descriptionMaxRune {
		return cleaned
	}
	return string(runes[:descriptionMaxRune]) + "…"
}

// sortResults orders a section: higher score first, then alphabetical label.
// Stable so callers can rely on deterministic ordering across calls with the
// same DB state (helpful for the regression suite).
func sortResults(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return strings.ToLower(results[i].Label) < strings.ToLower(results[j].Label)
	})
}
