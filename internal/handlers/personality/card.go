package personality

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // decoders for cover images that are not already PNG
	_ "image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/stcard"
	"go.uber.org/zap"
)

// maxCardBytes bounds an imported character card. Real cards are tens of KB; lorebook-heavy ones
// reach a few MB.
const maxCardBytes = 8 << 20

// ImportCharacterCard creates a personality from a SillyTavern chara_card_v2 (or v3) card sent as
// the request body, either as JSON or as a PNG with the card embedded. The conversion rules live
// in internal/stcard. A PNG's picture is not stored here: the client attaches it as the cover.
func (h *Handler) ImportCharacterCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCardBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet,
				fmt.Sprintf("Character card exceeds maximum size of %d MiB", maxCardBytes>>20), err)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// MaxPromptUnits and TextLimitHardMax are both UTF-16 code units; keep them aligned.
	imported, err := stcard.Parse(raw, stcard.ImportOptions{MaxPromptUnits: handlerutils.TextLimitHardMax})
	if err != nil {
		code := handlerutils.CodeNotSet
		if errors.Is(err, stcard.ErrPromptTooLong) {
			code = models.ErrCodeSystemPromptTooLong
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, code, err.Error(), err)
		return
	}

	isFirstPersonality := h.hasNoPersonalities(r.Context(), userID)

	created, err := h.ds.CreatePersonality(r.Context(), userID, models.Personality{
		Name:          h.uniquePersonalityName(r.Context(), userID, imported.Name),
		SystemPrompt:  imported.SystemPrompt,
		ImageStyle:    "auto",
		CharacterCard: &models.PersonalityCard{Data: imported.Data, OmittedFields: imported.OmittedFields},
	})
	if err != nil {
		h.logger.Error("failed to import character card",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to import character card", err)
		return
	}
	if isFirstPersonality {
		h.makeDefault(r.Context(), userID, created)
	}

	lore := imported.LoreFiles
	if lore == nil {
		lore = []stcard.LoreEntry{}
	}
	warnings := imported.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	handlerutils.RefreshResponseWriteDeadline(w, 60*time.Second)
	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, models.PersonalityCardImportResult{
		Personality: *created,
		LoreFiles:   lore,
		Warnings:    warnings,
	})
}

// uniquePersonalityName returns name, or "name (2)", "name (3)", ... when the user already has a
// personality with that name (compared case-insensitively). A listing failure is non-fatal: the
// original name is used, since duplicate names are allowed.
func (h *Handler) uniquePersonalityName(ctx context.Context, userID uuid.UUID, name string) string {
	taken := make(map[string]bool)
	const pageSize = 100
	for page := 1; ; page++ {
		res, err := h.ds.ListPersonalities(ctx, userID, page, pageSize, models.PersonalityFilters{Name: &name})
		if err != nil {
			h.logger.Warn("failed to check personality names before import; keeping the card's name",
				zap.String("user_id", userID.String()),
				zap.Error(err))
			return name
		}
		for _, item := range res.Results {
			if p, ok := item.(*models.Personality); ok {
				taken[strings.ToLower(strings.TrimSpace(p.Name))] = true
			}
		}
		if page*pageSize >= res.TotalCount {
			break
		}
	}

	candidate := name
	for n := 2; taken[strings.ToLower(candidate)]; n++ {
		candidate = fmt.Sprintf("%s (%d)", name, n)
	}
	return candidate
}

// ExportCharacterCard downloads a personality as a SillyTavern chara_card_v2 JSON document.
func (h *Handler) ExportCharacterCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	personalityID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return
	}

	p, err := h.ds.GetPersonality(r.Context(), userID, personalityID)
	if errors.Is(err, datastore.ErrPersonalityNotFound) || ent.IsNotFound(err) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get personality for card export",
			zap.String("user_id", userID.String()),
			zap.String("personality_id", personalityID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get personality", err)
		return
	}
	stored, err := h.ds.GetPersonalityCard(r.Context(), userID, personalityID)
	if err != nil {
		h.logger.Error("failed to load character card for export",
			zap.String("personality_id", personalityID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load character card", err)
		return
	}
	account, err := h.ds.GetUserByID(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to load user for card export", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load user", err)
		return
	}

	in := stcard.ExportInput{Name: p.Name, SystemPrompt: p.SystemPrompt, Creator: account.Username}
	if stored != nil {
		in.Card, in.OmittedFields = stored.Data, stored.OmittedFields
	}
	body, err := stcard.Export(in)
	if err != nil {
		h.logger.Error("failed to build character card", zap.String("personality_id", personalityID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to build character card", err)
		return
	}

	contentType, extension := "application/json; charset=utf-8", ".json"
	if r.URL.Query().Get("format") == "png" {
		pngBody, ok := h.embedInCover(w, r, userID, p, body)
		if !ok {
			return
		}
		body, contentType, extension = pngBody, "image/png", ".png"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": stcard.FileStem(p.Name) + extension}))
	w.Header().Set("Cache-Control", "no-cache")
	if _, err := w.Write(body); err != nil {
		h.logger.Warn("failed to write character card response", zap.String("personality_id", personalityID.String()), zap.Error(err))
	}
}

// embedInCover returns the personality's cover image as a PNG with the card JSON embedded in it
// (the SillyTavern PNG card format). It writes the error response itself and reports false when it
// cannot: a personality with no cover image has nothing to embed the card in.
func (h *Handler) embedInCover(w http.ResponseWriter, r *http.Request, userID uuid.UUID, p *models.Personality, cardJSON []byte) ([]byte, bool) {
	if p.CoverImageID == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet,
			"This personality has no cover image to embed the card in; export it as JSON instead", nil)
		return nil, false
	}
	att, err := h.ds.GetFileAttachment(r.Context(), userID, *p.CoverImageID)
	if err != nil {
		h.logger.Error("failed to load cover image for card export", zap.String("personality_id", p.ID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load cover image", err)
		return nil, false
	}
	img, _ := handlerutils.ResolveImageBytes(r.Context(), h.logger, h.fileStore, userID, att, false)
	if len(img) == 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read cover image", nil)
		return nil, false
	}
	asPNG, err := toPNG(img)
	if err == nil {
		img, err = stcard.EmbedInPNG(asPNG, cardJSON)
	}
	if err != nil {
		h.logger.Error("failed to embed card in cover image", zap.String("personality_id", p.ID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to build PNG card", err)
		return nil, false
	}
	return img, true
}

// toPNG returns img unchanged when it already is a PNG, else re-encodes it as one.
func toPNG(img []byte) ([]byte, error) {
	if stcard.IsPNG(img) {
		return img, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(img))
	if err != nil {
		return nil, fmt.Errorf("decode cover image: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, decoded); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
