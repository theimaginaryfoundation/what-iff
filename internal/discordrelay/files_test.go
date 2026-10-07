package discordrelay

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type fakeFiles map[string][]byte

func (f fakeFiles) DownloadFile(_ context.Context, key string) ([]byte, error) { return f[key], nil }

// replyWithFiles saves a reply carrying the named attachments, whose stored bytes
// have the given sizes.
func (h *harness) replyWithFiles(text string, sizes map[string]int) uuid.UUID {
	files := fakeFiles{}
	var atts []*models.FileAttachment
	for name, size := range sizes {
		key := "k/" + name
		files[key] = []byte(strings.Repeat("x", size))
		atts = append(atts, &models.FileAttachment{ID: uuid.New(), Name: name, FileType: "image/png", S3Key: key})
	}
	h.svc.Files = files
	id := uuid.New()
	h.store.messages[id] = &models.ChatMessage{ID: id, Message: text, Attachments: atts}
	return id
}

func TestAGeneratedImageIsUploadedWithTheReply(t *testing.T) {
	h := newHarness(t)
	id := h.replyWithFiles("Here you go", map[string]int{"cat.png": 1200})

	h.svc.post(h.ctx, *h.target, id, "c", "m1", nil)

	assert.Equal(t, []post{{channel: "c", replyTo: "m1", parts: []string{"Here you go"}}}, h.discord.allPosts())
	assert.Equal(t, [][]string{{"cat.png:1200"}}, h.discord.files)
	assert.Equal(t, models.DiscordLinkSent, h.store.outbound()[0].Status)
}

func TestAnImageWithNoTextIsStillPosted(t *testing.T) {
	h := newHarness(t)
	id := h.replyWithFiles("", map[string]int{"cat.png": 10})

	h.svc.post(h.ctx, *h.target, id, "c", "", nil)

	assert.Equal(t, []string{""}, h.discord.allPosts()[0].parts)
	assert.Equal(t, [][]string{{"cat.png:10"}}, h.discord.files)
}

func TestFilesOverDiscordsLimitAreNamedInANoteNotDropped(t *testing.T) {
	h := newHarness(t)
	id := h.replyWithFiles("Done", map[string]int{"huge.png": MaxUploadBytes + 1})

	h.svc.post(h.ctx, *h.target, id, "c", "", nil)

	posts := h.discord.allPosts()
	require.Len(t, posts, 1)
	assert.Contains(t, posts[0].parts[0], "Done")
	assert.Contains(t, posts[0].parts[0], "huge.png")
	assert.Equal(t, [][]string{nil}, h.discord.files, "nothing was uploaded")
}

func TestAReplyWithNothingToSendPostsNothing(t *testing.T) {
	h := newHarness(t)
	id := h.replyWithFiles("", nil)
	h.svc.post(h.ctx, *h.target, id, "c", "", nil)
	assert.Empty(t, h.discord.allPosts())
}

func TestWithoutAnObjectStoreTheReplyIsTextOnly(t *testing.T) {
	h := newHarness(t)
	id := h.replyWithFiles("Here", map[string]int{"cat.png": 10})
	h.svc.Files = nil

	h.svc.post(h.ctx, *h.target, id, "c", "", nil)

	assert.Equal(t, []string{"Here"}, h.discord.allPosts()[0].parts)
	assert.Equal(t, [][]string{nil}, h.discord.files)
}

func TestMissingAttachPermissionFallsBackToTextAndLeavesTheBindingAlone(t *testing.T) {
	h := newHarness(t)
	id := h.replyWithFiles("Here", map[string]int{"cat.png": 10})
	h.discord.errs = []error{ErrNoAccess} // the upload is refused; the text-only retry succeeds

	h.svc.post(h.ctx, *h.target, id, "c", "", nil)

	posts := h.discord.allPosts()
	require.Len(t, posts, 1)
	assert.Contains(t, posts[0].parts[0], "Here")
	assert.Contains(t, posts[0].parts[0], "permission to attach files")
	assert.Equal(t, [][]string{{"cat.png:10"}, nil}, h.discord.files)
	assert.Empty(t, h.store.bindStatus, "the binding is not marked broken")
	assert.Equal(t, models.DiscordLinkSent, h.store.outbound()[0].Status)
}

func TestFilesAreResentOnARetry(t *testing.T) {
	h := newHarness(t)
	id := h.replyWithFiles("Here", map[string]int{"cat.png": 10})
	h.discord.errs = []error{assert.AnError}

	h.svc.post(h.ctx, *h.target, id, "c", "", nil)

	assert.Equal(t, [][]string{{"cat.png:10"}, {"cat.png:10"}}, h.discord.files, "the retry carries the bytes again")
}

func TestOnlyTenFilesGoInOneMessage(t *testing.T) {
	h := newHarness(t)
	sizes := map[string]int{}
	for i := 0; i < MaxUploadFiles+2; i++ {
		sizes[strings.Repeat("a", i+1)+".png"] = 5
	}
	id := h.replyWithFiles("Many", sizes)

	h.svc.post(h.ctx, *h.target, id, "c", "", nil)

	assert.Len(t, h.discord.files[0], MaxUploadFiles)
	assert.Contains(t, h.discord.allPosts()[0].parts[0], "Couldn't attach")
}
