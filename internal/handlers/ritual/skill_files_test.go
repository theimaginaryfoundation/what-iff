package ritual

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"
)

func uploadSkillFile(t *testing.T, router *mux.Router, userID, ritualID uuid.UUID, relPath string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	require.NoError(t, mw.WriteField("path", relPath))
	part, err := mw.CreateFormFile("file", "upload")
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	req := asUser(httptest.NewRequest(http.MethodPost, "/ritual/"+ritualID.String()+"/files", &body), userID)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func createSkill(t *testing.T, router *mux.Router, userID uuid.UUID, name string) *models.Ritual {
	t.Helper()
	rec := postJSON(t, router, userID, http.MethodPost, "/ritual", map[string]any{"name": name, "description": "d", "content": "c"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var r models.Ritual
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &r))
	return &r
}

func TestUploadSkillFile_EnforcesTheBundleByteLimit(t *testing.T) {
	_, router, userID := newTestHandler(t)
	skill := createSkill(t, router, userID, "Big")
	full := bytes.Repeat([]byte("x"), skills.MaxBundleFileBytes)
	for i, p := range []string{"assets/a.bin", "assets/b.bin", "assets/c.bin"} {
		rec := uploadSkillFile(t, router, userID, skill.ID, p, full)
		require.Equal(t, http.StatusCreated, rec.Code, "upload %d: %s", i, rec.Body.String())
	}
	rec := uploadSkillFile(t, router, userID, skill.ID, "assets/d.bin", []byte("one byte too many"))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "30 MB")

	// Replacing a file with a smaller one fits again.
	rec = uploadSkillFile(t, router, userID, skill.ID, "assets/c.bin", []byte("small"))
	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, rec.Code, rec.Body.String())
}

func TestUploadSkillFile_RefusesTheReservedSkillMD(t *testing.T) {
	_, router, userID := newTestHandler(t)
	skill := createSkill(t, router, userID, "Notes")
	rec := uploadSkillFile(t, router, userID, skill.ID, "SKILL.md", []byte("---\nname: x\n---\n"))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "SKILL.md")
}

func TestUploadSkillFile_ReplaceKeepsAnObjectSomethingElseShares(t *testing.T) {
	h, router, userID := newTestHandler(t)
	store := h.files.(*memFileStore)
	skill := createSkill(t, router, userID, "Diagrams")
	require.Equal(t, http.StatusCreated, uploadSkillFile(t, router, userID, skill.ID, "assets/diagram.png", []byte("v1")).Code)
	files, err := h.ds.ListSkillFiles(context.Background(), userID, skill.ID)
	require.NoError(t, err)
	sharedKey := files[0].S3Key

	// A chat reply that showed the image shares its object.
	_, err = h.ds.CreateFileAttachment(context.Background(), userID, models.FileAttachment{
		Name: "diagram.png", FileType: "image/png", S3Key: sharedKey, ContextOnly: true,
	})
	require.NoError(t, err)

	rec := uploadSkillFile(t, router, userID, skill.ID, "assets/diagram.png", []byte("v2"))
	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, rec.Code, rec.Body.String())
	require.Equal(t, []byte("v1"), store.objects[sharedKey], "the reply's image must survive the replace")

	// With nothing else referencing it, the replaced object is released.
	files, err = h.ds.ListSkillFiles(context.Background(), userID, skill.ID)
	require.NoError(t, err)
	unsharedKey := files[0].S3Key
	rec = uploadSkillFile(t, router, userID, skill.ID, "assets/diagram.png", []byte("v3"))
	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, rec.Code, rec.Body.String())
	_, kept := store.objects[unsharedKey]
	require.False(t, kept)
}

// failingStore fails uploads after the first n.
type failingStore struct {
	*memFileStore
	n int
}

func (f *failingStore) UploadFile(ctx context.Context, key string, content []byte, ct string) error {
	if f.n <= 0 {
		return errors.New("s3 unavailable")
	}
	f.n--
	return f.memFileStore.UploadFile(ctx, key, content, ct)
}

func TestReimport_StorageFailureKeepsThePreviousFiles(t *testing.T) {
	h, router, userID := newTestHandler(t)
	importZip := func(entries map[string]string, ritualID string) ImportResult {
		fields := map[string]string{}
		if ritualID != "" {
			fields["ritual_id"] = ritualID
		}
		body, ctype := multipartFile(t, "file", "bundle.zip", zipBundle(t, entries), fields)
		req := asUser(httptest.NewRequest(http.MethodPost, "/ritual/import", body), userID)
		req.Header.Set("Content-Type", ctype)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Contains(t, []int{http.StatusOK, http.StatusCreated}, rec.Code, rec.Body.String())
		var res ImportResult
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		return res
	}
	first := importZip(map[string]string{"pdf-forms/SKILL.md": testSkillMD, "pdf-forms/scripts/a.py": "a", "pdf-forms/scripts/b.py": "b"}, "")
	require.Equal(t, 2, first.FilesImported)

	mem := h.files.(*memFileStore)
	h.files = &failingStore{memFileStore: mem, n: 1}
	res := importZip(map[string]string{"pdf-forms/SKILL.md": testSkillMD, "pdf-forms/scripts/c.py": "c", "pdf-forms/scripts/d.py": "d"}, first.Ritual.ID.String())
	require.Zero(t, res.FilesImported)
	require.Contains(t, strings.Join(res.Warnings, "\n"), "keeps its previous files")
	files, err := h.ds.ListSkillFiles(context.Background(), userID, first.Ritual.ID)
	require.NoError(t, err)
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
		require.Contains(t, mem.objects, f.S3Key, "the kept files' objects are still there")
	}
	require.Equal(t, []string{"a.py", "b.py"}, names)

	// A bundle with no files leaves the skill with none.
	h.files = mem
	res = importZip(map[string]string{"pdf-forms/SKILL.md": testSkillMD}, first.Ritual.ID.String())
	require.Zero(t, res.FilesImported)
	files, err = h.ds.ListSkillFiles(context.Background(), userID, first.Ritual.ID)
	require.NoError(t, err)
	require.Empty(t, files)
}
