package sharelink

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

const testKey = "AAAAAAAAAAAAAAAAAAAAAA" // 22 characters

// fakeStore is one owner's gallery and one share, with attempts and bans in memory.
type fakeStore struct {
	memGate
	owner    uuid.UUID
	files    []models.FileAttachment
	shared   *models.FileAttachment
	passcode string
	gen      int
}

func (f *fakeStore) SharedItemByKey(_ context.Context, key string) (*models.SharedItem, error) {
	if key != testKey || f.shared == nil {
		return nil, models.ErrShareLinkNotFound
	}
	ct := ""
	if f.passcode != "" {
		ct = "sealed"
	}
	return &models.SharedItem{Key: key, OwnerID: f.owner, File: f.shared, PasscodeCiphertext: ct, PasscodeGeneration: f.gen}, nil
}
func (f *fakeStore) SharePasscode(*models.SharedItem) (string, error) { return f.passcode, nil }
func (f *fakeStore) SharedProjectFiles(_ context.Context, owner uuid.UUID, root string) ([]models.FileAttachment, error) {
	var out []models.FileAttachment
	for _, file := range f.files {
		if owner == f.owner && models.FolderIsWithin(file.Folder, root) {
			out = append(out, file)
		}
	}
	return out, nil
}
func (f *fakeStore) SharedProjectFile(_ context.Context, owner uuid.UUID, root, folder, name string) (*models.FileAttachment, error) {
	for i, file := range f.files {
		if owner == f.owner && models.FolderIsWithin(folder, root) && file.Folder == folder && file.Name == name {
			return &f.files[i], nil
		}
	}
	return nil, datastore.ErrFileAttachmentNotFound
}
func (f *fakeStore) ShareLinkForFile(context.Context, uuid.UUID, uuid.UUID) (*models.ShareLink, error) {
	return nil, models.ErrShareLinkNotFound
}
func (f *fakeStore) CreateShareLink(context.Context, uuid.UUID, uuid.UUID, bool) (*models.ShareLink, bool, error) {
	return nil, false, nil
}
func (f *fakeStore) UpdateShareLink(context.Context, uuid.UUID, uuid.UUID, *bool, bool) (*models.ShareLink, error) {
	return nil, nil
}
func (f *fakeStore) DeleteShareLink(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeStore) ListShareLinks(context.Context, uuid.UUID) ([]models.ShareLink, error) {
	return nil, nil
}

// memFiles holds each file's bytes under its key.
type memFiles map[string][]byte

func (m memFiles) UploadFile(_ context.Context, key string, b []byte, _ string) error {
	m[key] = b
	return nil
}
func (m memFiles) DownloadFile(_ context.Context, key string) ([]byte, error) { return m[key], nil }
func (m memFiles) DeleteFile(_ context.Context, key string) error             { delete(m, key); return nil }

func setup(t *testing.T) (*fakeStore, http.Handler) {
	t.Helper()
	owner := uuid.New()
	file := func(folder, name string) models.FileAttachment {
		return models.FileAttachment{ID: uuid.New(), Folder: folder, Name: name, FileType: "text/plain", S3Key: folder + "/" + name, ContentVersion: 1}
	}
	page := file("site", "index.html")
	page.FileType = "text/html"
	s := &fakeStore{
		owner:    owner,
		passcode: "Abc123Def456",
		gen:      1,
		files: []models.FileAttachment{
			page,
			file("site/css", "app.css"),
			file("other", "secret.txt"),
			file("sites", "lookalike.txt"),
		},
	}
	s.shared = &s.files[0]
	files := memFiles{"site/index.html": []byte("<h1>hi</h1>"), "site/css/app.css": []byte("h1{}"), "other/secret.txt": []byte("secret")}
	h := NewHandler(s, func() storage.FileStore { return files }, zap.NewNop(), Config{Secret: "test-secret"})
	r := mux.NewRouter()
	h.RegisterPublicRoutes(r.PathPrefix("/api").Subrouter())
	return s, r
}

func call(h http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.RemoteAddr = "203.0.113.7:1234"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSharedWebApp_LockedUntilUnlocked_AndConfinedToItsFolder(t *testing.T) {
	_, h := setup(t)
	base := "/api/shared/" + testKey

	// Locked: nothing about the share.
	rec := call(h, "GET", base, "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.JSONEq(t, `{"locked":true}`, rec.Body.String())
	require.Equal(t, http.StatusUnauthorized, call(h, "GET", base+"/files/index.html", "").Code)

	// A wrong passcode says how many tries are left; the right one gives the share's cookie.
	rec = call(h, "POST", base+"/unlock", `{"passcode":"abc123def456"}`)
	require.Equal(t, http.StatusUnauthorized, rec.Code, "passcodes are case-sensitive")
	require.Contains(t, rec.Body.String(), `"attempts_left":4`)
	rec = call(h, "POST", base+"/unlock", `{"passcode":"Abc123Def456"}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
	cookie := rec.Result().Cookies()[0]

	rec = call(h, "GET", base, "", cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"name":"index.html","file_type":"text/html","content_version":1,"web_app":true,"page":"index.html","project_name":"site"}`, rec.Body.String())

	rec = call(h, "GET", base+"/files", "", cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	var listing struct{ Files []sharedFile }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listing))
	var paths []string
	for _, f := range listing.Files {
		paths = append(paths, f.Path)
	}
	require.Equal(t, []string{"index.html", "css/app.css"}, paths, "only the folder's files, not a lookalike folder's")

	rec = call(h, "GET", base+"/files/CSS/app.css", "", cookie)
	require.Equal(t, http.StatusOK, rec.Code, "folder segments match without case, as the gallery's")
	require.Equal(t, "h1{}", rec.Body.String())
	require.Equal(t, "sandbox; default-src 'none'", rec.Header().Get("Content-Security-Policy"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "same-origin", rec.Header().Get("Cross-Origin-Resource-Policy"), "no other origin may load it")

	for _, p := range []string{"../other/secret.txt", "css/../../other/secret.txt", "%2e%2e/other/secret.txt", "..%2fother%2fsecret.txt"} {
		rec = call(h, "GET", base+"/files/"+p, "", cookie)
		require.NotEqual(t, http.StatusOK, rec.Code, p)
		require.NotContains(t, rec.Body.String(), "secret", p)
	}
	require.Equal(t, http.StatusNotFound, call(h, "GET", base+"/files/secret.txt", "", cookie).Code)
}

func TestSharedFile_SingleFileShareHasNoProject(t *testing.T) {
	s, h := setup(t)
	s.files[0].Folder = "" // the page moved to the top level: a single file now
	s.passcode = ""
	base := "/api/shared/" + testKey
	rec := call(h, "GET", base, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"web_app":false`)
	require.Equal(t, http.StatusNotFound, call(h, "GET", base+"/files", "").Code)
	require.Equal(t, http.StatusNotFound, call(h, "GET", "/api/shared/BBBBBBBBBBBBBBBBBBBBBB", "").Code)
}

func TestUnlock_BansAnAddressAfterFiveWrongPasscodes(t *testing.T) {
	s, h := setup(t)
	base := "/api/shared/" + testKey
	for i := 0; i < 4; i++ {
		require.Equal(t, http.StatusUnauthorized, call(h, "POST", base+"/unlock", `{"passcode":"nope"}`).Code)
	}
	rec := call(h, "POST", base+"/unlock", `{"passcode":"nope"}`)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "3600", rec.Header().Get("Retry-After"))
	// Banned: even the right passcode is refused, the same way.
	rec = call(h, "POST", base+"/unlock", `{"passcode":"Abc123Def456"}`)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Empty(t, rec.Result().Cookies())

	// A new passcode signs out whoever unlocked before.
	s.bans = nil
	s.attempts = nil
	rec = call(h, "POST", base+"/unlock", `{"passcode":"Abc123Def456"}`)
	cookie := rec.Result().Cookies()[0]
	require.Equal(t, http.StatusOK, call(h, "GET", base, "", cookie).Code)
	s.gen++
	require.Equal(t, http.StatusUnauthorized, call(h, "GET", base, "", cookie).Code)
}
