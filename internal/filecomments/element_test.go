package filecomments

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestCleanElement(t *testing.T) {
	page := uuid.NewString()
	require.Nil(t, cleanElement(nil))
	require.Nil(t, cleanElement(&models.FileCommentElement{Page: "index.html", Selector: "#hero"}), "a page is an HTML file's id")
	require.Nil(t, cleanElement(&models.FileCommentElement{Page: page, Selector: "  "}))
	require.Nil(t, cleanElement(&models.FileCommentElement{Page: page, Selector: strings.Repeat("a", MaxElementSelectorRunes+1)}))

	attrs := map[string]string{"data-testid": "signup", "class": strings.Repeat("x", 500), "": "dropped"}
	for i := 0; i < 20; i++ {
		attrs["data-extra-"+string(rune('a'+i))] = "v"
	}
	got := cleanElement(&models.FileCommentElement{
		Page:        " " + page + " ",
		Selector:    " [data-testid=\"signup\"] ",
		Fingerprint: &models.FileCommentElementFingerprint{Tag: "BUTTON", Text: strings.Repeat("é", 300), Attrs: attrs},
	})
	require.Equal(t, page, got.Page)
	require.Equal(t, `[data-testid="signup"]`, got.Selector)
	require.Equal(t, "BUTTON", got.Fingerprint.Tag, "kept as the page reported it: SVG names have capitals (foreignObject)")
	require.Equal(t, maxElementTextRunes, len([]rune(got.Fingerprint.Text)))
	require.LessOrEqual(t, len(got.Fingerprint.Attrs), maxElementAttrs)
	// The attributes kept past the limit are the first by name, every time.
	again := cleanElement(&models.FileCommentElement{Page: page, Selector: "x", Fingerprint: &models.FileCommentElementFingerprint{Tag: "p", Attrs: attrs}})
	require.Equal(t, got.Fingerprint.Attrs, again.Fingerprint.Attrs)
	require.Contains(t, got.Fingerprint.Attrs, "class")
	require.NotContains(t, got.Fingerprint.Attrs, "")
	if class, ok := got.Fingerprint.Attrs["class"]; ok {
		require.Len(t, class, maxElementAttrRunes)
	}

	// A fingerprint whose tag is not one is dropped; the anchor stays.
	got = cleanElement(&models.FileCommentElement{Page: page, Selector: "main > p", Fingerprint: &models.FileCommentElementFingerprint{Tag: "<script>"}})
	require.NotNil(t, got)
	require.Nil(t, got.Fingerprint)
}

func TestAPagesElementThreadKeepsBothAnchorsAcrossAnEdit(t *testing.T) {
	svc, store, files := setup(line)
	store.file.Name, store.file.FileType = "index.html", "text/html"
	ctx := context.Background()
	page := store.file.ID.String()
	onElement := thread("el", 11, 25)
	onElement.Element = &models.FileCommentElement{Page: page, Selector: "#title", Fingerprint: &models.FileCommentElementFingerprint{Tag: "h1", Text: "Hello"}}
	onlyElement := models.FileCommentThread{ID: "js", Element: &models.FileCommentElement{Page: page, Selector: "ul > li:nth-of-type(2)"}}

	got, err := svc.Write(ctx, uuid.New(), store.file.ID, []models.FileCommentThread{onElement, onlyElement}, 1, 0)
	require.NoError(t, err)
	require.Equal(t, "inline comment", got.Threads[0].Quote)
	require.Equal(t, "#title", got.Threads[0].Element.Selector)
	// An element a script made has no code in this file: element anchor only.
	require.Nil(t, got.Threads[1].Anchor)
	require.Equal(t, "ul > li:nth-of-type(2)", got.Threads[1].Element.Selector)

	// The file is edited elsewhere: the code anchor is carried, the element anchor kept as it was.
	files["k2"] = []byte("I've updated this inline comment in Go source code")
	store.file.S3Key, store.file.ContentVersion = "k2", 2
	got, err = svc.Read(ctx, uuid.New(), store.file.ID)
	require.NoError(t, err)
	require.Equal(t, "inline comment", got.Threads[0].Quote)
	require.Equal(t, "#title", got.Threads[0].Element.Selector)
	require.Equal(t, "ul > li:nth-of-type(2)", got.Threads[1].Element.Selector)
}

func TestAnImagesOrDiagramsThreadsAreNeverOnAPagesElement(t *testing.T) {
	ctx := context.Background()
	files := memFiles{"img1": {0x89, 'P', 'N', 'G'}}
	store := &fakeStore{file: &models.FileAttachment{ID: uuid.New(), S3Key: "img1", Name: "photo.png", FileType: "image/png", ContentVersion: 1}}
	svc := New(store, files)
	th := thread("t1", 0, 3)
	th.Element = &models.FileCommentElement{Page: uuid.NewString(), Selector: "img"}
	got, err := svc.Write(ctx, uuid.New(), store.file.ID, []models.FileCommentThread{th}, 1, 0)
	require.NoError(t, err)
	require.Nil(t, got.Threads[0].Element)

	require.Nil(t, OnCells([]models.FileCommentThread{th}, "")[0].Element)
}
