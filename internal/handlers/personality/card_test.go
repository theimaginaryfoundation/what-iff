package personality

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// cardStore is a fakeStore that records what a card import writes.
type cardStore struct {
	*fakeStore
	created     []models.Personality
	existing    []*models.Personality
	defaultedTo uuid.UUID
}

func newCardStore() *cardStore {
	cs := &cardStore{}
	cs.fakeStore = &fakeStore{
		listPersonalitiesFn: func(_ context.Context, _ uuid.UUID, page, _ int, f models.PersonalityFilters) (*models.PaginatedResponse, error) {
			items := make([]any, 0, len(cs.existing))
			for _, p := range cs.existing {
				items = append(items, p)
			}
			return &models.PaginatedResponse{Results: items, TotalCount: len(items), Page: page}, nil
		},
		createPersonalityFn: func(_ context.Context, _ uuid.UUID, p models.Personality) (*models.Personality, error) {
			cs.created = append(cs.created, p)
			p.ID = uuid.New()
			return &p, nil
		},
		getUserPreferencesFn: func(context.Context, uuid.UUID) (*models.UserPreferences, error) {
			return &models.UserPreferences{}, nil
		},
		updateUserPrefsFn: func(_ context.Context, _ uuid.UUID, prefs models.UserPreferences) (*models.UserPreferences, error) {
			cs.defaultedTo = prefs.DefaultPersonalityID
			return &prefs, nil
		},
	}
	return cs
}

func serveCard(t *testing.T, store Store, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	NewHandler(store, zap.NewNop(), nil).RegisterRoutes(router)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func cardJSON(data string) string {
	return `{"spec":"chara_card_v2","spec_version":"2.0","data":` + data + `}`
}

func TestImportCharacterCard_CreatesPersonalityWithPassthroughBlob(t *testing.T) {
	t.Parallel()
	cs := newCardStore()

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", cardJSON(
		`{"name":"Ada","system_prompt":"You are Ada.","description":"A fox.","first_mes":"Hello!","tags":["fox"]}`))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Len(t, cs.created, 1)
	got := cs.created[0]
	require.Equal(t, "Ada", got.Name)
	require.Equal(t, "You are Ada.\n\n### [Description]\nA fox.", got.SystemPrompt)
	require.Equal(t, "Hello!", got.CharacterCard.Data["first_mes"], "greeting must ride in the blob, not the prompt")
	require.NotContains(t, got.SystemPrompt, "Hello!")

	var resp struct {
		Personality map[string]any `json:"personality"`
		LoreFiles   []any          `json:"lore_files"`
		Warnings    []any          `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "Ada", resp.Personality["name"])
	require.NotNil(t, resp.LoreFiles, "lore_files must serialize as [] not null")
	require.NotNil(t, resp.Warnings, "warnings must serialize as [] not null")
	require.NotContains(t, w.Body.String(), "character_card", "the blob must never reach API responses")
}

func TestImportCharacterCard_FirstPersonalityBecomesDefault(t *testing.T) {
	t.Parallel()
	cs := newCardStore() // no existing personalities

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", cardJSON(`{"name":"Ada","description":"d"}`))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.NotEqual(t, uuid.Nil, cs.defaultedTo)
}

func TestImportCharacterCard_LaterPersonalityLeavesDefaultAlone(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	cs.existing = []*models.Personality{{ID: uuid.New(), Name: "Someone else"}}

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", cardJSON(`{"name":"Ada","description":"d"}`))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, uuid.Nil, cs.defaultedTo)
}

func TestImportCharacterCard_UniquifiesCollidingName(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	cs.existing = []*models.Personality{{Name: "Ada"}, {Name: "ada (2)"}}

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", cardJSON(`{"name":"Ada","description":"d"}`))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "Ada (3)", cs.created[0].Name)
}

func TestImportCharacterCard_BigCharacterBookComesBackAsLoreFiles(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	entries := make([]string, 0, 6)
	for i := 1; i <= 6; i++ {
		entries = append(entries, fmt.Sprintf(`{"keys":["k%d"],"comment":"Entry %d","content":"Body %d","enabled":true}`, i, i, i))
	}

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", cardJSON(
		`{"name":"Ada","description":"d","character_book":{"entries":[`+strings.Join(entries, ",")+`]}}`))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.NotContains(t, cs.created[0].SystemPrompt, "Body 1", "entries beyond the threshold are files, not prompt text")
	var resp models.PersonalityCardImportResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.LoreFiles, 6)
	require.Equal(t, "Entry 1.md", resp.LoreFiles[0].FileName)
	require.Equal(t, "Body 1", resp.LoreFiles[0].Content)
}

func TestImportCharacterCard_RejectsBadCards(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body     string
		wantCode int
		wantErr  string
	}{
		"not json":        {`nope`, http.StatusBadRequest, ""},
		"unknown spec":    {`{"spec":"chara_card_v9","data":{}}`, http.StatusBadRequest, ""},
		"no prompt":       {cardJSON(`{"name":"Ada","first_mes":"hi"}`), http.StatusBadRequest, ""},
		"prompt too long": {cardJSON(`{"name":"Ada","description":"` + strings.Repeat("a", 26_000) + `"}`), http.StatusBadRequest, models.ErrCodeSystemPromptTooLong},
		"body too large":  {strings.Repeat("x", maxCardBytes+1), http.StatusRequestEntityTooLarge, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cs := newCardStore()
			w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", tc.body)
			require.Equal(t, tc.wantCode, w.Code, w.Body.String())
			if tc.wantErr != "" {
				require.Contains(t, w.Body.String(), tc.wantErr)
			}
			require.Empty(t, cs.created, "a rejected card must not create anything")
		})
	}
}

func TestImportCharacterCard_DatastoreFailureIs500(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	cs.createPersonalityFn = func(context.Context, uuid.UUID, models.Personality) (*models.Personality, error) {
		return nil, fmt.Errorf("db down")
	}

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", cardJSON(`{"name":"Ada","description":"d"}`))

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestExportCharacterCard_NativePersonality(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	cs.getPersonalityFn = func(_ context.Context, _, id uuid.UUID) (*models.Personality, error) {
		return &models.Personality{ID: id, Name: "Vera Calder", SystemPrompt: "You are Vera."}, nil
	}

	w := serveCard(t, cs, http.MethodGet, "/personality/"+uuid.NewString()+"/export/sillytavern", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Header().Get("Content-Disposition"), `filename="Vera Calder.json"`)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")
	var card struct {
		Spec        string         `json:"spec"`
		SpecVersion string         `json:"spec_version"`
		Data        map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
	require.Equal(t, "chara_card_v2", card.Spec)
	require.Equal(t, "2.0", card.SpecVersion)
	require.Equal(t, "Vera Calder", card.Data["name"])
	require.Equal(t, "You are Vera.", card.Data["system_prompt"])
	require.Equal(t, "tester", card.Data["creator"], "creator is the WhatIff username of the owner")
}

func TestExportCharacterCard_ReconstructsFromBlobAndKeepsOriginalAuthor(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	cs.getPersonalityFn = func(_ context.Context, _, id uuid.UUID) (*models.Personality, error) {
		return &models.Personality{ID: id, Name: "Ada", SystemPrompt: "SYS\n\n### [Description]\nA fox."}, nil
	}
	cs.getPersonalityCardFn = func(context.Context, uuid.UUID, uuid.UUID) (*models.PersonalityCard, error) {
		return &models.PersonalityCard{Data: map[string]any{"creator": "original", "first_mes": "Hello!", "tags": []any{"fox"}, "future": "kept"}}, nil
	}

	w := serveCard(t, cs, http.MethodGet, "/personality/"+uuid.NewString()+"/export/sillytavern", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var card struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
	require.Equal(t, "SYS", card.Data["system_prompt"])
	require.Equal(t, "A fox.", card.Data["description"])
	require.Equal(t, "original", card.Data["creator"])
	require.Equal(t, "Hello!", card.Data["first_mes"])
	require.Equal(t, "kept", card.Data["future"])
	require.Contains(t, card.Data["creator_notes"], "structural")
}

func TestExportCharacterCard_NotFoundAndBadID(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	cs.getPersonalityFn = func(context.Context, uuid.UUID, uuid.UUID) (*models.Personality, error) {
		return nil, datastore.ErrPersonalityNotFound
	}

	require.Equal(t, http.StatusNotFound, serveCard(t, cs, http.MethodGet, "/personality/"+uuid.NewString()+"/export/sillytavern", "").Code)
	require.Equal(t, http.StatusBadRequest, serveCard(t, cs, http.MethodGet, "/personality/not-a-uuid/export/sillytavern", "").Code)
}
