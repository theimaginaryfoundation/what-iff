package ritual

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

// Skill bundle files (ADR 0x035 §3): a skill's reference documents, scripts and assets, kept as
// file_attachments rows with origin = skill under skills/<slug>/… and stored under
// users/{uid}/skills/{skill_id}/. Any content type; never chunked; read-only to agents and to
// gallery moves. The skill editor and import manage them through these routes.

// SkillFile is a bundle file as the API reports it.
type SkillFile struct {
	ID       uuid.UUID `json:"id"`
	Path     string    `json:"path"`
	FileType string    `json:"file_type"`
	// Folder and Name are the file's gallery location (skills/<slug>/<subdir>, name), the path the
	// agent reads it at.
	Folder    string    `json:"folder"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// ListSkillFiles handles GET /ritual/{id}/files.
func (h *Handler) ListSkillFiles(w http.ResponseWriter, r *http.Request) {
	userID, ritual, ok := h.ownedRitual(w, r)
	if !ok {
		return
	}
	files, err := h.ds.ListSkillFiles(r.Context(), userID, ritual.ID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list skill files", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, skillFilesResponse(ritual, files))
}

func skillFilesResponse(ritual *models.Ritual, files []*models.FileAttachment) []SkillFile {
	out := make([]SkillFile, 0, len(files))
	for _, f := range files {
		out = append(out, SkillFile{
			ID: f.ID, Path: datastore.SkillFileRelPath(f, ritualSlug(ritual)), FileType: f.FileType,
			Folder: f.Folder, Name: f.Name, CreatedAt: f.CreatedAt,
		})
	}
	return out
}

// UploadSkillFile handles POST /ritual/{id}/files: a multipart form with `file` and `path` (the
// file's place in the bundle, e.g. references/FORMS.md; defaults to references/<filename>). A file
// already at that path is replaced.
func (h *Handler) UploadSkillFile(w http.ResponseWriter, r *http.Request) {
	userID, ritual, ok := h.ownedRitual(w, r)
	if !ok {
		return
	}
	if h.files == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotImplemented, handlerutils.CodeNotSet, "This deployment has no file storage for skill files", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(skills.MaxBundleFileBytes)+(1<<20))
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet,
				fmt.Sprintf("Skill file too large (max %d MB)", skills.MaxBundleFileBytes>>20), nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid multipart form (expected a 'file' part)", err)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	f, fh, err := r.FormFile("file")
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Missing 'file' part", err)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(skills.MaxBundleFileBytes)+1))
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Could not read the file", err)
		return
	}
	if len(data) > skills.MaxBundleFileBytes {
		handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet,
			fmt.Sprintf("Skill file too large (max %d MB)", skills.MaxBundleFileBytes>>20), nil)
		return
	}
	relPath := strings.TrimSpace(r.FormValue("path"))
	if relPath == "" {
		relPath = path.Join("references", path.Base(fh.Filename))
	}
	stored, err := h.storeSkillFile(r.Context(), userID, ritual, skills.File{Path: relPath, Data: data}, true)
	if err != nil {
		h.respondSkillError(w, err, "Failed to store skill file")
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, skillFilesResponse(ritual, []*models.FileAttachment{stored})[0])
}

// DeleteSkillFile handles DELETE /ritual/{id}/files/{fileId}.
func (h *Handler) DeleteSkillFile(w http.ResponseWriter, r *http.Request) {
	userID, ritual, ok := h.ownedRitual(w, r)
	if !ok {
		return
	}
	fileID, err := uuid.Parse(mux.Vars(r)["fileId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid file ID", err)
		return
	}
	removed, err := h.ds.DeleteSkillFiles(r.Context(), userID, ritual.ID, []uuid.UUID{fileID})
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete skill file", err)
		return
	}
	if len(removed) == 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Skill file not found", nil)
		return
	}
	h.releaseSkillObjects(r.Context(), userID, removed)
	w.WriteHeader(http.StatusNoContent)
}

// ownedRitual resolves {id} to one of the caller's rituals, answering the error itself.
func (h *Handler) ownedRitual(w http.ResponseWriter, r *http.Request) (uuid.UUID, *models.Ritual, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, nil, false
	}
	ritualID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual ID", err)
		return uuid.Nil, nil, false
	}
	ritual, err := h.ds.GetRitual(r.Context(), userID, ritualID)
	if err == datastore.ErrRitualNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Ritual not found", nil)
		return uuid.Nil, nil, false
	} else if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load skill", err)
		return uuid.Nil, nil, false
	}
	return userID, ritual, true
}

func ritualSlug(r *models.Ritual) string {
	if s := strings.TrimSpace(r.Slug); s != "" {
		return s
	}
	return skills.Slugify(r.Name)
}

// storeSkillFile uploads one bundle file for the skill and records it under skills/<slug>/<path>.
// With replace, a file already at that path gets the new content (its old object is released);
// without, it is an error. The bundle's limits are enforced against what the skill already holds.
func (h *Handler) storeSkillFile(ctx context.Context, userID uuid.UUID, ritual *models.Ritual, file skills.File, replace bool) (*models.FileAttachment, error) {
	if h.files == nil {
		return nil, fmt.Errorf("%w: this deployment has no file storage for skill files", skills.ErrInvalid)
	}
	if len(file.Data) > skills.MaxBundleFileBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d MB", skills.ErrInvalid, file.Path, skills.MaxBundleFileBytes>>20)
	}
	folder, name, err := datastore.SkillFileFolder(ritualSlug(ritual), file.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: bundle path %q: %v", skills.ErrInvalid, file.Path, err)
	}
	if name, err = models.NormalizeFileName(name, name); err != nil {
		return nil, fmt.Errorf("%w: bundle path %q: %v", skills.ErrInvalid, file.Path, err)
	}
	existing, err := h.ds.FindSkillFile(ctx, userID, ritual.ID, folder, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		files, err := h.ds.ListSkillFiles(ctx, userID, ritual.ID)
		if err != nil {
			return nil, err
		}
		if len(files) >= skills.MaxBundleFiles {
			return nil, fmt.Errorf("%w: this skill already holds %d files, the limit", skills.ErrInvalid, skills.MaxBundleFiles)
		}
	} else if !replace {
		return nil, fmt.Errorf("%w: a file is already at %s", skills.ErrInvalid, file.Path)
	}

	fileType := detectSkillFileType(name, file.Data)
	objectID := uuid.New()
	key := storage.FileKeyForSkillFile(userID, ritual.ID, objectID, name)
	if err := h.files.UploadFile(ctx, key, file.Data, fileType); err != nil {
		return nil, fmt.Errorf("store %s: %w", file.Path, err)
	}
	if existing != nil {
		oldKey, err := h.ds.ReplaceSkillFileObject(ctx, userID, ritual.ID, existing.ID, key, fileType)
		if err != nil {
			h.dropObject(ctx, key)
			return nil, err
		}
		if oldKey != "" && oldKey != key {
			h.dropObject(ctx, oldKey)
		}
		existing.S3Key, existing.FileType = key, fileType
		return existing, nil
	}
	created, err := h.ds.CreateSkillFile(ctx, userID, ritual.ID, folder, name, fileType, key)
	if err != nil {
		h.dropObject(ctx, key)
		return nil, err
	}
	return created, nil
}

// storeBundleFiles stores every file of an imported bundle for the skill, replacing what the skill
// held. It returns how many were stored and the problems it hit (the import goes on past a bad file).
func (h *Handler) storeBundleFiles(ctx context.Context, userID uuid.UUID, ritual *models.Ritual, files []skills.File) (int, []string) {
	if len(files) == 0 || h.files == nil {
		var warnings []string
		if len(files) > 0 {
			warnings = append(warnings, fmt.Sprintf("%d bundle file(s) were not stored: this deployment has no file storage", len(files)))
		}
		return 0, warnings
	}
	var total int
	for _, f := range files {
		total += len(f.Data)
	}
	if total > skills.MaxBundleBytes {
		return 0, []string{fmt.Sprintf("bundle files were not stored: together they exceed %d MB", skills.MaxBundleBytes>>20)}
	}
	removed, err := h.ds.DeleteSkillFiles(ctx, userID, ritual.ID, nil)
	if err != nil {
		return 0, []string{"could not replace the skill's existing files: " + err.Error()}
	}
	h.releaseSkillObjects(ctx, userID, removed)
	stored := 0
	var warnings []string
	for _, f := range files {
		if _, err := h.storeSkillFile(ctx, userID, ritual, f, true); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s was not stored: %s", f.Path, strings.TrimPrefix(err.Error(), skills.ErrInvalid.Error()+": ")))
			continue
		}
		stored++
	}
	return stored, warnings
}

// releaseSkillObjects deletes the objects of removed bundle file rows that nothing else references.
func (h *Handler) releaseSkillObjects(ctx context.Context, userID uuid.UUID, removed []models.FileAttachment) {
	if h.files == nil || len(removed) == 0 {
		return
	}
	storage.ReleaseAttachmentObjects(context.WithoutCancel(ctx), h.logger, h.files, h.ds, userID, removed)
}

func (h *Handler) dropObject(ctx context.Context, key string) {
	if err := h.files.DeleteFile(context.WithoutCancel(ctx), key); err != nil {
		h.logger.Warn("skill file: failed to delete an unused object", zap.String("key", key), zap.Error(err))
	}
}

// detectSkillFileType picks a content type from the name, then from the bytes. Scripts without a
// registered extension are stored as text so agent_file_read can show them.
func detectSkillFileType(name string, data []byte) string {
	ext := strings.ToLower(path.Ext(name))
	if t, ok := skillFileTypes[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		if i := strings.Index(t, ";"); i > 0 {
			t = t[:i]
		}
		return t
	}
	sniffed := http.DetectContentType(data)
	if i := strings.Index(sniffed, ";"); i > 0 {
		sniffed = sniffed[:i]
	}
	return sniffed
}

// skillFileTypes are content types for bundle files the Go mime table does not know or gets wrong.
var skillFileTypes = map[string]string{
	".md":    "text/markdown",
	".py":    "text/x-python",
	".sh":    "text/x-shellscript",
	".js":    "text/javascript",
	".ts":    "text/typescript",
	".yaml":  "application/yaml",
	".yml":   "application/yaml",
	".toml":  "application/toml",
	".jsonl": "application/jsonl",
	".csv":   "text/csv",
	".txt":   "text/plain",
	".sql":   "application/sql",
	".r":     "text/x-r",
	".rb":    "text/x-ruby",
}
