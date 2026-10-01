package personality

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/stcard"
	"go.uber.org/zap"
)

// memFileStore serves one fixed object for every key.
type memFileStore struct{ data []byte }

func (m memFileStore) UploadFile(context.Context, string, []byte, string) error { return nil }
func (m memFileStore) DownloadFile(context.Context, string) ([]byte, error)     { return m.data, nil }
func (m memFileStore) DeleteFile(context.Context, string) error                 { return nil }

func tinyImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(2, 2, color.RGBA{G: 200, A: 255})
	return img
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, tinyImage()))
	return buf.Bytes()
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, tinyImage(), nil))
	return buf.Bytes()
}

// serveWithCover routes a request through a handler whose file store holds cover.
func serveWithCover(store Store, cover []byte, target string) *httptest.ResponseRecorder {
	h := NewHandler(store, zap.NewNop(), nil)
	h.fileStore = memFileStore{data: cover}
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func storeWithCover(coverID *uuid.UUID, card *models.PersonalityCard) *cardStore {
	cs := newCardStore()
	cs.getPersonalityFn = func(_ context.Context, _, id uuid.UUID) (*models.Personality, error) {
		return &models.Personality{ID: id, Name: "Ada", SystemPrompt: "You are Ada.", CoverImageID: coverID}, nil
	}
	cs.getPersonalityCardFn = func(context.Context, uuid.UUID, uuid.UUID) (*models.PersonalityCard, error) { return card, nil }
	cs.getFileAttachmentFn = func(_ context.Context, _, id uuid.UUID) (*models.FileAttachment, error) {
		return &models.FileAttachment{ID: id, Name: "cover.png", FileType: "image/png"}, nil
	}
	return cs
}

func TestImportCharacterCard_AcceptsAPNGCard(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	cardJSON := cardJSON(`{"name":"Ada","system_prompt":"You are Ada.","first_mes":"Hello!"}`)
	body, err := stcard.EmbedInPNG(tinyPNG(t), []byte(cardJSON))
	require.NoError(t, err)

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", string(body))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "You are Ada.", cs.created[0].SystemPrompt)
	require.Equal(t, "Hello!", cs.created[0].CharacterCard.Data["first_mes"])
}

func TestImportCharacterCard_PNGWithoutACardIs400(t *testing.T) {
	t.Parallel()
	cs := newCardStore()

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", string(tinyPNG(t)))

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "no embedded character card")
	require.Empty(t, cs.created)
}

func TestImportCharacterCard_OverLimitDropsScenarioAndTellsTheUser(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	body := cardJSON(`{"name":"Ada","description":"` + strings.Repeat("d", 20_000) + `","scenario":"` + strings.Repeat("s", 10_000) + `"}`)

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", body)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	created := cs.created[0]
	require.NotContains(t, created.SystemPrompt, stcard.HeaderScenario)
	require.Equal(t, []string{"scenario"}, created.CharacterCard.OmittedFields)
	require.Len(t, created.CharacterCard.Data["scenario"], 10_000, "the omitted text must stay in the stored card")
	var resp models.PersonalityCardImportResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Warnings, 1)
	require.Contains(t, resp.Warnings[0], "scenario")
}

func TestImportCharacterCard_StillTooLongIsASpecificError(t *testing.T) {
	t.Parallel()
	cs := newCardStore()
	body := cardJSON(`{"name":"Ada","description":"` + strings.Repeat("d", 30_000) + `"}`)

	w := serveCard(t, cs, http.MethodPost, "/personality/import/sillytavern", body)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), models.ErrCodeSystemPromptTooLong)
	require.Contains(t, w.Body.String(), "limit of 25,000")
	require.Empty(t, cs.created)
}

func TestExportCharacterCard_PNGEmbedsTheCardInTheCoverImage(t *testing.T) {
	t.Parallel()
	coverID := uuid.New()
	store := storeWithCover(&coverID, &models.PersonalityCard{Data: map[string]any{"first_mes": "Hello!"}})

	w := serveWithCover(store, tinyPNG(t), "/personality/"+uuid.NewString()+"/export/sillytavern?format=png")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "image/png", w.Header().Get("Content-Type"))
	require.Contains(t, w.Header().Get("Content-Disposition"), `filename=Ada.png`)
	_, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err, "the export must still be a viewable image")

	embedded, err := stcard.ExtractFromPNG(w.Body.Bytes())
	require.NoError(t, err)
	imported, err := stcard.Parse(embedded, stcard.ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, "Ada", imported.Name)
	require.Equal(t, "Hello!", imported.Data["first_mes"])
}

func TestExportCharacterCard_PNGConvertsANonPNGCover(t *testing.T) {
	t.Parallel()
	coverID := uuid.New()
	store := storeWithCover(&coverID, nil)

	w := serveWithCover(store, tinyJPEG(t), "/personality/"+uuid.NewString()+"/export/sillytavern?format=png")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err := stcard.ExtractFromPNG(w.Body.Bytes())
	require.NoError(t, err)
}

func TestExportCharacterCard_PNGWithoutACoverIsExplained(t *testing.T) {
	t.Parallel()
	store := storeWithCover(nil, nil)

	w := serveWithCover(store, nil, "/personality/"+uuid.NewString()+"/export/sillytavern?format=png")

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "no cover image")
}

func TestExportCharacterCard_RestoresOmittedFields(t *testing.T) {
	t.Parallel()
	store := storeWithCover(nil, &models.PersonalityCard{
		Data:          map[string]any{"scenario": "The original scenario."},
		OmittedFields: []string{"scenario"},
	})

	w := serveWithCover(store, nil, "/personality/"+uuid.NewString()+"/export/sillytavern")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var card struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
	require.Equal(t, "The original scenario.", card.Data["scenario"])
}
