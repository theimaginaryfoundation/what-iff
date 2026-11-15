package sharelink

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

// sharedView is what a visitor is told about a share once it is open to them.
type sharedView struct {
	Name           string `json:"name"`
	FileType       string `json:"file_type"`
	SizeBytes      *int64 `json:"size_bytes,omitempty"`
	ContentVersion int    `json:"content_version"`
	// WebApp is true for a web app: its files are listed at /files, and Page is the page's path.
	WebApp bool   `json:"web_app"`
	Page   string `json:"page,omitempty"`
	// ProjectName is the web app's folder's own name (not its path).
	ProjectName string `json:"project_name,omitempty"`
}

// sharedFile is one file of a shared web app, by its path in the project.
type sharedFile struct {
	Path           string `json:"path"`
	FileType       string `json:"file_type"`
	SizeBytes      *int64 `json:"size_bytes,omitempty"`
	ContentVersion int    `json:"content_version"`
}

// open is the share the request names, when the visitor may see it: false after answering
// otherwise (404 for no such share, 401 for one that needs its passcode).
func (h *Handler) open(w http.ResponseWriter, r *http.Request) (*models.SharedItem, bool) {
	noStore(w)
	item, err := h.ds.SharedItemByKey(r.Context(), mux.Vars(r)["key"])
	if errors.Is(err, models.ErrShareLinkNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "This link does not exist or is no longer shared.", nil)
		return nil, false
	}
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to open the share", err)
		return nil, false
	}
	if item.PasscodeCiphertext != "" && !unlocked(r, h.sessionKey, item.Key, item.PasscodeGeneration, h.now()) {
		// Nothing about the share before it is unlocked: not its name, kind or size.
		handlerutils.RespondWithJSON(w, h.logger, http.StatusUnauthorized, map[string]bool{"locked": true})
		return nil, false
	}
	return item, true
}

// GetShared handles GET /shared/{key}: what the share is, or that it needs its passcode.
func (h *Handler) GetShared(w http.ResponseWriter, r *http.Request) {
	item, ok := h.open(w, r)
	if !ok {
		return
	}
	f := item.File
	view := sharedView{Name: f.Name, FileType: f.FileType, SizeBytes: f.SizeBytes, ContentVersion: f.ContentVersion}
	if models.SharesAsWebApp(f) {
		view.WebApp, view.Page = true, f.Name
		view.ProjectName = f.Folder[strings.LastIndexByte(f.Folder, '/')+1:]
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, view)
}

// Unlock handles POST /shared/{key}/unlock: a passcode, limited and banned by address (ADR 0x051
// §4). The right one is answered with the share's cookie. Only the share site's own requests are
// taken (a JSON body, and same-origin where the browser says), so another site cannot spend a
// visitor's tries and get their address banned.
func (h *Handler) Unlock(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	if !sameOriginJSON(r) {
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Unlock from the share site", nil)
		return
	}
	ctx := r.Context()
	now := h.now()
	prefix, ok := visitorAddress(r, h.clientIPHeader, h.proxySecret)
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Could not tell where the request came from", nil)
		return
	}
	var body struct {
		Passcode string `json:"passcode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", nil)
		return
	}
	addr := addressKey(h.addressSecret, prefix)
	id, wait, err := admit(ctx, h.ds, addr, now)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to check the passcode", err)
		return
	}
	if wait > 0 {
		h.tooMany(w, wait)
		return
	}
	item, err := h.ds.SharedItemByKey(ctx, mux.Vars(r)["key"])
	if err != nil && !errors.Is(err, models.ErrShareLinkNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to check the passcode", err)
		return
	}
	right := false
	switch {
	case item != nil && item.PasscodeCiphertext == "":
		// Nothing to unlock: no guess was made.
		right = true
	case item != nil:
		passcode, err := h.ds.SharePasscode(item)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to check the passcode", err)
			return
		}
		right = passcode != "" && subtle.ConstantTimeCompare([]byte(body.Passcode), []byte(passcode)) == 1
	}
	// A key that names no share is a wrong guess too, so keys cannot be probed for free.
	ban, left, err := conclude(ctx, h.ds, addr, id, right, now)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to check the passcode", err)
		return
	}
	switch {
	case right && item.PasscodeCiphertext == "":
		w.WriteHeader(http.StatusNoContent)
	case right:
		setSession(w, h.sessionKey, item.Key, item.PasscodeGeneration, now)
		w.WriteHeader(http.StatusNoContent)
	case ban > 0:
		h.tooMany(w, ban)
	case item == nil:
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "This link does not exist or is no longer shared.", nil)
	default:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusUnauthorized, map[string]any{"error": "That passcode is not right.", "attempts_left": left})
	}
}

// sameOriginJSON reports whether a request can be the share site's own: a JSON body (which a page
// of another origin cannot send without a preflight the API refuses) and, where the browser says
// where it came from, the same origin. A request without Sec-Fetch-Site (an older browser, a
// script) is judged by its body alone.
func sameOriginJSON(r *http.Request) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		return false
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site == "" || site == "same-origin"
}

func (h *Handler) tooMany(w http.ResponseWriter, wait time.Duration) {
	seconds := int(math.Ceil(wait.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	handlerutils.RespondWithJSON(w, h.logger, http.StatusTooManyRequests, map[string]any{
		"error":       "Too many tries. Try again later.",
		"retry_after": seconds,
	})
}

// GetSharedContent handles GET /shared/{key}/content: the shared file's bytes (a web app's page).
func (h *Handler) GetSharedContent(w http.ResponseWriter, r *http.Request) {
	item, ok := h.open(w, r)
	if !ok {
		return
	}
	h.writeFile(w, r, item, item.File)
}

// ListSharedFiles handles GET /shared/{key}/files: a shared web app's files, by their paths in
// the project (ADR 0x051 §7); the newest file takes a path several have.
func (h *Handler) ListSharedFiles(w http.ResponseWriter, r *http.Request) {
	item, ok := h.open(w, r)
	if !ok {
		return
	}
	root, ok := h.webAppRoot(w, item)
	if !ok {
		return
	}
	files, err := h.ds.SharedProjectFiles(r.Context(), item.OwnerID, root)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list the files", err)
		return
	}
	seen := make(map[string]bool, len(files))
	out := make([]sharedFile, 0, len(files))
	for _, f := range files {
		path, inside := projectPath(root, f)
		if !inside || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, sharedFile{Path: path, FileType: f.FileType, SizeBytes: f.SizeBytes, ContentVersion: f.ContentVersion})
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, map[string]any{"files": out})
}

// GetSharedFile handles GET /shared/{key}/files/{path}: one file of a shared web app, found only
// inside its folder (ADR 0x051 §7).
func (h *Handler) GetSharedFile(w http.ResponseWriter, r *http.Request) {
	item, ok := h.open(w, r)
	if !ok {
		return
	}
	root, ok := h.webAppRoot(w, item)
	if !ok {
		return
	}
	folder, name, ok := projectPlace(root, mux.Vars(r)["path"])
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Not a path in this project", nil)
		return
	}
	file, err := h.ds.SharedProjectFile(r.Context(), item.OwnerID, root, folder, name)
	if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Not in this project", nil)
		return
	}
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the file", err)
		return
	}
	h.writeFile(w, r, item, file)
}

// webAppRoot is a shared web app's folder: the page's folder now (the share follows the page).
func (h *Handler) webAppRoot(w http.ResponseWriter, item *models.SharedItem) (string, bool) {
	if !models.SharesAsWebApp(item.File) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "This share is a single file", nil)
		return "", false
	}
	return item.File.Folder, true
}

// writeFile answers with a file's bytes so that they cannot run on the share site (ADR 0x051 §5):
// as a download, unsniffed, under a policy that allows nothing.
func (h *Handler) writeFile(w http.ResponseWriter, r *http.Request, item *models.SharedItem, f *models.FileAttachment) {
	store := h.files()
	if store == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File storage not configured", nil)
		return
	}
	data, err := storage.ReadAttachment(r.Context(), store, storage.AttachmentObject{
		UserID: item.OwnerID, ID: f.ID, S3Key: f.S3Key, Name: f.Name, FileType: f.FileType,
		ChatID: f.ChatID, PersonalityID: f.PersonalityID,
	})
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the file", err)
		return
	}
	contentType := strings.TrimSpace(f.FileType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("X-Content-Version", strconv.Itoa(f.ContentVersion))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// noStore keeps a share's answers out of every cache: they depend on the visitor's cookie, and a
// link that stops being shared must stop working at once. They are for the share site alone: a
// page on another origin (a shared web app on the web apps site, which is the same site) cannot
// load them as a script or an image either.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
}
