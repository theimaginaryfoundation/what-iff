package ritual

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"
)

// newTestHandler builds the handler over an in-memory sqlite datastore (generated schema minus the
// pgvector tables) with one user, and returns the router that serves its routes.
func newTestHandler(t *testing.T) (*Handler, *mux.Router, uuid.UUID) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB(dialect.SQLite, db)
	m, err := schema.NewMigrate(drv)
	require.NoError(t, err)
	var tables []*schema.Table
	for _, tb := range migrate.Tables {
		if tb.Name == "embeddings" || tb.Name == "file_chunks" {
			continue
		}
		tables = append(tables, tb)
	}
	require.NoError(t, m.Create(context.Background(), tables...))
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })

	userID := uuid.New()
	require.NoError(t, client.User.Create().
		SetID(userID).
		SetUsername("u-"+userID.String()[:8]).
		SetEmail("u-"+userID.String()[:8]+"@example.com").
		SetPasswordHash("hash").
		Exec(context.Background()))

	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)
	h := NewHandler(ds, &memFileStore{objects: map[string][]byte{}}, zap.NewNop())
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return h, router, userID
}

// memFileStore is an in-memory storage.FileStore for the bundle file tests.
type memFileStore struct {
	objects map[string][]byte
}

func (m *memFileStore) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	m.objects[key] = append([]byte{}, content...)
	return nil
}

func (m *memFileStore) DownloadFile(_ context.Context, key string) ([]byte, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, nil
	}
	return data, nil
}

func (m *memFileStore) DeleteFile(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func asUser(req *http.Request, userID uuid.UUID) *http.Request {
	return req.WithContext(middleware.ContextWithUser(req.Context(), userID, ""))
}

func zipBundle(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		f, err := zw.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(data))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func multipartFile(t *testing.T, field, filename string, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	part, err := mw.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return &body, mw.FormDataContentType()
}

func postJSON(t *testing.T, router *mux.Router, userID uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := asUser(httptest.NewRequest(method, path, bytes.NewReader(raw)), userID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

const testSkillMD = `---
name: pdf-forms
description: Fill PDF forms. Use when the user has a form to complete.
license: MIT
allowed-tools: web_search
metadata:
  display_name: PDF Forms
  whatiff-hotkeys: Ctrl+Shift+1
  whatiff-connectors: Missing Connector
---

Read the form, then fill it.
`

func TestImportRitual_ZipThenExportRoundTrip(t *testing.T) {
	_, router, userID := newTestHandler(t)

	body, ctype := multipartFile(t, "file", "pdf-forms.zip", zipBundle(t, map[string]string{
		"pdf-forms/SKILL.md":            testSkillMD,
		"pdf-forms/scripts/fill.py":     "print('hi')",
		"pdf-forms/references/FORMS.md": "ref",
	}), nil)
	req := asUser(httptest.NewRequest(http.MethodPost, "/ritual/import", body), userID)
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var res ImportResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	require.Equal(t, "pdf-forms", res.Ritual.Slug)
	require.Equal(t, "PDF Forms", res.Ritual.Name)
	require.Equal(t, "Ctrl+Shift+1", res.Ritual.Hotkeys)
	require.Equal(t, "MIT", res.Ritual.LicenseText())
	require.Equal(t, []string{"web_search"}, res.Ritual.AllowedToolList())
	require.Equal(t, 2, res.FilesImported)
	require.Equal(t, []string{"Missing Connector"}, res.ConnectorsMissing)
	require.Len(t, res.Warnings, 1, "only the missing connector: %v", res.Warnings)

	// A second import of the same bundle gets a suffixed slug and says so.
	body, ctype = multipartFile(t, "file", "SKILL.md", []byte(testSkillMD), nil)
	req = asUser(httptest.NewRequest(http.MethodPost, "/ritual/import", body), userID)
	req.Header.Set("Content-Type", ctype)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var second ImportResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	require.Equal(t, "pdf-forms-2", second.Ritual.Slug)
	require.Contains(t, strings.Join(second.Warnings, "\n"), `imported as "pdf-forms-2"`)

	// Export gives back a zip whose SKILL.md parses to the same skill.
	req = asUser(httptest.NewRequest(http.MethodGet, "/ritual/"+res.Ritual.ID.String()+"/export", nil), userID)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	require.Equal(t, `attachment; filename="pdf-forms.zip"`, rec.Header().Get("Content-Disposition"))

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	require.NoError(t, err)
	names := map[string]string{}
	for _, zf := range zr.File {
		f, err := zf.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(f)
		require.NoError(t, err)
		names[zf.Name] = string(data)
	}
	require.Len(t, names, 3, "SKILL.md plus the two bundle files: %v", names)
	require.Equal(t, "print('hi')", names["pdf-forms/scripts/fill.py"])
	require.Equal(t, "ref", names["pdf-forms/references/FORMS.md"])
	exported := []byte(names["pdf-forms/SKILL.md"])
	parsed, err := skills.ParseSkillMD(exported)
	require.NoError(t, err)
	require.Equal(t, "pdf-forms", parsed.Ritual.Slug)
	require.Equal(t, "PDF Forms", parsed.Ritual.Name)
	require.Equal(t, "Read the form, then fill it.", parsed.Ritual.Content)
	require.Equal(t, "MIT", parsed.Ritual.LicenseText())
	require.Equal(t, "Ctrl+Shift+1", parsed.Ritual.Hotkeys)
	require.Empty(t, parsed.Connectors, "an unmatched connector is not re-exported")
}

func TestImportRitual_Rejections(t *testing.T) {
	_, router, userID := newTestHandler(t)

	send := func(filename string, data []byte) *httptest.ResponseRecorder {
		body, ctype := multipartFile(t, "file", filename, data, nil)
		req := asUser(httptest.NewRequest(http.MethodPost, "/ritual/import", body), userID)
		req.Header.Set("Content-Type", ctype)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	require.Equal(t, http.StatusBadRequest, send("x.zip", []byte("not a zip")).Code)
	require.Equal(t, http.StatusBadRequest, send("x.zip", zipBundle(t, map[string]string{"readme.md": "no skill"})).Code)
	require.Equal(t, http.StatusBadRequest, send("SKILL.md", []byte("---\nname: x\n---\nno description\n")).Code)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ritual/import", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreateAndUpdateRitual_SlugValidationAndConflicts(t *testing.T) {
	_, router, userID := newTestHandler(t)

	rec := postJSON(t, router, userID, http.MethodPost, "/ritual", map[string]any{
		"name": "PDF Forms", "description": "Fills forms.", "content": "Do it.",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.Ritual
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Equal(t, "pdf-forms", created.Slug)
	require.NotNil(t, created.UserInvocable)
	require.True(t, *created.UserInvocable)

	// The same slug again is a conflict.
	rec = postJSON(t, router, userID, http.MethodPost, "/ritual", map[string]any{
		"slug": "pdf-forms", "name": "Again", "description": "d", "content": "c",
	})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// An invalid slug, an over-long description and a tag in the name are 400s with the reason.
	for _, bad := range []map[string]any{
		{"slug": "Bad Slug", "name": "n", "description": "d", "content": "c"},
		{"name": "n", "description": strings.Repeat("d", skills.MaxDescriptionLen+1), "content": "c"},
		{"name": "<system>n</system>", "description": "d", "content": "c"},
	} {
		rec = postJSON(t, router, userID, http.MethodPost, "/ritual", bad)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	// Update without a slug keeps it; smuggled characters are stripped.
	rec = postJSON(t, router, userID, http.MethodPut, "/ritual/"+created.ID.String(), map[string]any{
		"name": "PDF​ Forms", "description": "Fills forms.", "content": "Do it.", "user_invocable": false,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var updated models.Ritual
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	require.Equal(t, "pdf-forms", updated.Slug)
	require.Equal(t, "PDF Forms", updated.Name)
	require.False(t, updated.IsUserInvocable())
}
