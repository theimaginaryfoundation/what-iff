package ritual

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"
)

const (
	// maxImportBytes caps the whole import request: the bundle limit plus room for form overhead.
	maxImportBytes = skills.MaxBundleBytes + (1 << 20)
	// multipartMemory is the in-memory buffer for ParseMultipartForm; file parts beyond it spool to disk.
	multipartMemory = 32 << 10
)

// ImportResult is the response to POST /ritual/import.
type ImportResult struct {
	Ritual *models.Ritual `json:"ritual"`
	// Warnings are the lenient-parse repairs and import-time substitutions the user should see.
	Warnings []string `json:"warnings"`
	// FilesImported counts the bundle files stored under skills/<slug>/ (ADR 0x035 §3).
	FilesImported int `json:"files_imported"`
	// ConnectorsLinked and ConnectorsMissing report the exporter's connector names matched to this
	// account's connectors by name, and the ones with no match.
	ConnectorsLinked  []string `json:"connectors_linked"`
	ConnectorsMissing []string `json:"connectors_missing"`
}

// ImportRitual handles POST /ritual/import: a multipart form with either `file` (a skill zip, or a
// lone SKILL.md) or several `files` parts whose filenames are paths inside the skill directory.
// An optional `personality_id` field binds the new skill to a personality. A slug the account
// already uses gets a numeric suffix rather than failing, with a warning. With `ritual_id`, the
// bundle is imported into that existing skill instead: its text fields are replaced (the slug is
// kept) and its files are replaced by the bundle's.
func (h *Handler) ImportRitual(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet,
				fmt.Sprintf("Skill bundle too large (max %d MB)", skills.MaxBundleBytes>>20), nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet,
			"Invalid multipart form (expected multipart/form-data with a 'file' zip or 'files' parts)", err)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	bundle, err := readImportBundle(r.MultipartForm)
	if err != nil {
		h.respondSkillError(w, err, "Failed to read skill bundle")
		return
	}
	parsed, err := skills.ParseSkillMD(bundle.SkillMD)
	if err != nil {
		h.respondSkillError(w, err, "Failed to parse SKILL.md")
		return
	}

	var personalityID uuid.UUID
	if raw := strings.TrimSpace(r.FormValue("personality_id")); raw != "" {
		personalityID, err = uuid.Parse(raw)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_id", err)
			return
		}
	}

	result := ImportResult{
		Warnings:          append([]string{}, parsed.Warnings...),
		ConnectorsLinked:  []string{},
		ConnectorsMissing: []string{},
	}

	ritual := parsed.Ritual
	ritual.PersonalityID = personalityID
	var existing *models.Ritual
	if raw := strings.TrimSpace(r.FormValue("ritual_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual_id", err)
			return
		}
		existing, err = h.ds.GetRitual(r.Context(), userID, id)
		if err == datastore.ErrRitualNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Ritual not found", nil)
			return
		} else if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load skill", err)
			return
		}
		// The existing skill keeps its slug, personality and connectors; the bundle replaces the rest.
		if ritual.Slug != existing.Slug {
			result.Warnings = append(result.Warnings, fmt.Sprintf("the bundle names the skill %q; it stays %q here", ritual.Slug, existing.Slug))
		}
		ritual.ID, ritual.Slug, ritual.PersonalityID = existing.ID, existing.Slug, existing.PersonalityID
	} else {
		requested := ritual.Slug
		ritual.Slug, err = skills.UniqueSlug(requested, func(candidate string) (bool, error) {
			return h.ds.RitualSlugTaken(r.Context(), userID, candidate, uuid.Nil)
		})
		if err != nil {
			h.respondSkillError(w, err, "Failed to choose a slug")
			return
		}
		if ritual.Slug != requested {
			result.Warnings = append(result.Warnings, fmt.Sprintf("slug %q is already in use; imported as %q", requested, ritual.Slug))
		}
	}

	if len(parsed.Connectors) > 0 && existing == nil {
		ids, linked, missing, err := h.matchConnectorsByName(r, userID, parsed.Connectors)
		if err != nil {
			h.logger.Error("failed to match skill connectors", zap.String("user_id", userID.String()), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to match connectors", err)
			return
		}
		ritual.MCPServerIDs = &ids
		result.ConnectorsLinked, result.ConnectorsMissing = linked, missing
		if len(missing) > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("no connector named %s; link one in the editor", strings.Join(quoteAll(missing), ", ")))
		}
	}

	var saved *models.Ritual
	if existing != nil {
		saved, err = h.ds.UpdateRitual(r.Context(), userID, ritual)
	} else {
		saved, err = h.ds.CreateRitual(r.Context(), userID, ritual)
	}
	if err != nil {
		h.logger.Error("failed to import ritual", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to import skill", err)
		return
	}
	result.Ritual = saved
	stored, fileWarnings := h.storeBundleFiles(r.Context(), userID, saved, bundle.Files)
	result.FilesImported = stored
	result.Warnings = append(result.Warnings, fileWarnings...)
	status := http.StatusCreated
	if existing != nil {
		status = http.StatusOK
	}
	handlerutils.RespondWithJSON(w, h.logger, status, result)
}

// readImportBundle builds the bundle from the form: a `file` part that is a zip or a lone
// SKILL.md, or `files` parts named by their path in the skill directory.
func readImportBundle(form *multipart.Form) (*skills.Bundle, error) {
	if form == nil {
		return nil, fmt.Errorf("%w: no files in the request", skills.ErrInvalid)
	}
	if parts := form.File["file"]; len(parts) == 1 {
		fh := parts[0]
		f, err := fh.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: cannot open upload: %v", skills.ErrInvalid, err)
		}
		defer f.Close()
		if strings.EqualFold(fh.Filename, skills.SkillFileName) || strings.HasSuffix(strings.ToLower(fh.Filename), ".md") {
			data, err := io.ReadAll(io.LimitReader(f, int64(skills.MaxBundleFileBytes)+1))
			if err != nil {
				return nil, fmt.Errorf("%w: cannot read upload: %v", skills.ErrInvalid, err)
			}
			if len(data) > skills.MaxBundleFileBytes {
				return nil, fmt.Errorf("%w: SKILL.md is larger than %d MB", skills.ErrInvalid, skills.MaxBundleFileBytes>>20)
			}
			return skills.BundleFromFiles([]skills.File{{Path: skills.SkillFileName, Data: data}})
		}
		return skills.ReadBundle(f, fh.Size)
	}
	parts := form.File["files"]
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: send the skill as a 'file' zip or as 'files' parts", skills.ErrInvalid)
	}
	files := make([]skills.File, 0, len(parts))
	var total int64
	for _, fh := range parts {
		total += fh.Size
		if total > int64(skills.MaxBundleBytes) {
			return nil, fmt.Errorf("%w: bundle is larger than %d MB", skills.ErrInvalid, skills.MaxBundleBytes>>20)
		}
		f, err := fh.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: cannot open %s: %v", skills.ErrInvalid, fh.Filename, err)
		}
		data, err := io.ReadAll(io.LimitReader(f, int64(skills.MaxBundleFileBytes)+1))
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: cannot read %s: %v", skills.ErrInvalid, fh.Filename, err)
		}
		files = append(files, skills.File{Path: fh.Filename, Data: data})
	}
	return skills.BundleFromFiles(files)
}

// matchConnectorsByName maps the exporter's connector names onto this account's connectors,
// case-insensitively, returning the matched ids and the names on each side.
func (h *Handler) matchConnectorsByName(r *http.Request, userID uuid.UUID, names []string) (ids []uuid.UUID, linked, missing []string, err error) {
	page, err := h.ds.ListMCPServers(r.Context(), userID, 1, 500, models.MCPServerFilters{})
	if err != nil {
		return nil, nil, nil, err
	}
	byName := map[string]*models.MCPServer{}
	for _, item := range page.Results {
		if s, ok := item.(*models.MCPServer); ok && s != nil {
			byName[strings.ToLower(strings.TrimSpace(s.Name))] = s
		}
	}
	ids, linked, missing = []uuid.UUID{}, []string{}, []string{}
	for _, name := range names {
		if s, ok := byName[strings.ToLower(strings.TrimSpace(name))]; ok {
			ids = append(ids, s.ID)
			linked = append(linked, s.Name)
		} else {
			missing = append(missing, name)
		}
	}
	return ids, linked, missing, nil
}

// ExportRitual handles GET /ritual/{id}/export: the skill as an Agent Skills zip holding
// `<slug>/SKILL.md`, rendered from the stored fields, plus the bundle's files at their paths.
func (h *Handler) ExportRitual(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	ritualID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual ID", err)
		return
	}
	ritual, err := h.ds.GetRitual(r.Context(), userID, ritualID)
	if err == datastore.ErrRitualNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Ritual not found", nil)
		return
	} else if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load skill", err)
		return
	}

	var connectors []string
	if ritual.MCPServerIDs != nil && len(*ritual.MCPServerIDs) > 0 {
		servers, err := h.ds.ListRitualMCPServers(r.Context(), userID, []uuid.UUID{ritualID})
		if err != nil {
			h.logger.Warn("export: could not list the skill's connectors", zap.String("ritual_id", ritualID.String()), zap.Error(err))
		}
		for _, s := range servers {
			if s != nil && strings.TrimSpace(s.Name) != "" {
				connectors = append(connectors, s.Name)
			}
		}
	}

	slug := ritual.Slug
	if slug == "" {
		slug = skills.Slugify(ritual.Name)
	}
	skillMD, err := skills.RenderSkillMD(ritual, connectors)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to render SKILL.md", err)
		return
	}
	bundleFiles, err := h.exportSkillFiles(r.Context(), userID, ritual)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the skill's files", err)
		return
	}
	var buf bytes.Buffer
	if err := skills.WriteBundle(&buf, slug, skillMD, bundleFiles); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to build skill bundle", err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, slug))
	w.Header().Set("Content-Length", fmt.Sprint(buf.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// respondSkillError maps a skills.ErrInvalid to 400 with its detail, anything else to 500.
func (h *Handler) respondSkillError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, skills.ErrInvalid) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, invalidSkillMessage(err), nil)
		return
	}
	h.logger.Error(fallback, zap.Error(err))
	handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, fallback, err)
}

func quoteAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// exportSkillFiles reads the skill's bundle files from the object store for an export. A file whose
// object is missing is skipped with a log line rather than failing the export.
func (h *Handler) exportSkillFiles(ctx context.Context, userID uuid.UUID, ritual *models.Ritual) ([]skills.File, error) {
	rows, err := h.ds.ListSkillFiles(ctx, userID, ritual.ID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || h.files == nil {
		return nil, nil
	}
	out := make([]skills.File, 0, len(rows))
	for _, row := range rows {
		data, err := h.files.DownloadFile(ctx, row.S3Key)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", row.Name, err)
		}
		if data == nil {
			h.logger.Warn("export: skill file object missing", zap.String("file_attachment_id", row.ID.String()), zap.String("key", row.S3Key))
			continue
		}
		out = append(out, skills.File{Path: datastore.SkillFileRelPath(row, ritualSlug(ritual)), Data: data})
	}
	return out, nil
}
