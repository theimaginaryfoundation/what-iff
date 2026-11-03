package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

// historyImageStore serves content for every key, or only for the keys in byKey when set.
type historyImageStore struct {
	content []byte
	byKey   map[string][]byte
}

func (s historyImageStore) UploadFile(context.Context, string, []byte, string) error { return nil }
func (s historyImageStore) DownloadFile(_ context.Context, key string) ([]byte, error) {
	if s.byKey != nil {
		return s.byKey[key], nil
	}
	return s.content, nil
}
func (s historyImageStore) DeleteFile(context.Context, string) error { return nil }

func historyImageMessage() *models.ChatMessage {
	return &models.ChatMessage{
		Attachments: []*models.FileAttachment{
			{ID: uuid.New(), Name: "a.png", FileType: "image/png"},
		},
	}
}

// inlineAll selects every image on msg for inlining.
func inlineAll(msg *models.ChatMessage) historyImageSet {
	return selectHistoryImages(nil, []*models.ChatMessage{msg}, len(msg.Attachments))
}

func TestHistoryImagesForMessage_LoadsBytesFromTheStore(t *testing.T) {
	t.Parallel()

	var store storage.FileStore = historyImageStore{content: []byte("hello")}
	msg := historyImageMessage()
	imgs := historyImagesForMessage(t.Context(), nil, store, uuid.New(), uuid.New(), msg, inlineAll(msg))
	require.Len(t, imgs, 1)
	require.Equal(t, []byte("hello"), imgs[0].RawBytes)
}

// Prior turns send the stored thumbnail (a JPEG), not the full-size original.
func TestHistoryImagesForMessage_PrefersTheThumbnail(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	msg := historyImageMessage()
	att := msg.Attachments[0]
	var store storage.FileStore = historyImageStore{byKey: map[string][]byte{
		storage.FileKeyForImage(userID, att.ID, att.Name): []byte("full-size"),
		storage.FileKeyForImageThumbnail(userID, att.ID):  []byte("thumb"),
	}}
	imgs := historyImagesForMessage(t.Context(), nil, store, userID, uuid.New(), msg, inlineAll(msg))
	require.Len(t, imgs, 1)
	require.Equal(t, []byte("thumb"), imgs[0].RawBytes)
	require.Equal(t, "image/jpeg", imgs[0].MediaType)
}

// An image stored before thumbnails existed still goes in, as its original.
func TestHistoryImagesForMessage_FallsBackToTheOriginal(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	msg := historyImageMessage()
	att := msg.Attachments[0]
	var store storage.FileStore = historyImageStore{byKey: map[string][]byte{
		storage.FileKeyForImage(userID, att.ID, att.Name): []byte("full-size"),
	}}
	imgs := historyImagesForMessage(t.Context(), nil, store, userID, uuid.New(), msg, inlineAll(msg))
	require.Len(t, imgs, 1)
	require.Equal(t, []byte("full-size"), imgs[0].RawBytes)
	require.Equal(t, "image/png", imgs[0].MediaType)
}

// Images not selected for inlining (vision off, or past the cap) are left out, and the store
// is not touched.
func TestHistoryImagesForMessage_NoImagesWhenNotSelected(t *testing.T) {
	t.Parallel()

	var store storage.FileStore = historyImageStore{content: []byte("hello")}
	require.Empty(t, historyImagesForMessage(t.Context(), nil, store, uuid.New(), uuid.New(), historyImageMessage(), nil))
	require.Empty(t, historyImagesForMessage(t.Context(), nil, store, uuid.New(), uuid.New(), historyImageMessage(), historyImageSet{uuid.New(): {}}))
}

// An image whose object is missing from the store is skipped rather than sent empty.
func TestHistoryImagesForMessage_SkipsImagesMissingFromTheStore(t *testing.T) {
	t.Parallel()

	var store storage.FileStore = historyImageStore{}
	msg := historyImageMessage()
	require.Empty(t, historyImagesForMessage(t.Context(), nil, store, uuid.New(), uuid.New(), msg, inlineAll(msg)))
}

func imageMessage(n int) (*models.ChatMessage, []uuid.UUID) {
	msg := &models.ChatMessage{Attachments: []*models.FileAttachment{{ID: uuid.New(), Name: "notes.txt", FileType: "text/plain"}}}
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
		msg.Attachments = append(msg.Attachments, &models.FileAttachment{ID: ids[i], Name: "i.png", FileType: "image/png"})
	}
	return msg, ids
}

// The cap keeps the newest images: history newest-first, then carry-over (assistant reply
// before its user turn). Non-image attachments never count against it.
func TestSelectHistoryImages_KeepsTheMostRecent(t *testing.T) {
	t.Parallel()

	carryUser, carryUserIDs := imageMessage(1)
	carryReply, carryReplyIDs := imageMessage(1)
	oldest, oldestIDs := imageMessage(2)
	middle, middleIDs := imageMessage(2)
	newest, newestIDs := imageMessage(2)
	carryOver := [][2]*models.ChatMessage{{carryUser, carryReply}}
	history := []*models.ChatMessage{oldest, middle, newest}

	got := selectHistoryImages(carryOver, history, 5)
	require.Len(t, got, 5)
	for _, id := range append(append(newestIDs, middleIDs...), oldestIDs[1]) {
		require.Contains(t, got, id)
	}
	require.NotContains(t, got, oldestIDs[0], "within a message the later image is newer")

	got = selectHistoryImages(carryOver, history, 7)
	require.Contains(t, got, carryReplyIDs[0])
	require.NotContains(t, got, carryUserIDs[0], "the reply is newer than the turn it answers")

	require.Empty(t, selectHistoryImages(carryOver, history, 0))
}

func TestAppendHistoryTurn_WithImages(t *testing.T) {
	t.Parallel()

	var ctx provider.ModelContext
	ctx.AppendHistoryTurn(provider.RoleUser, "look at this", []provider.UserMessageImage{
		{MediaType: "image/png", RawBytes: []byte{1}},
	}, true)
	require.Len(t, ctx.Segments, 1)
	require.Len(t, ctx.Segments[0].UserImages, 1)
}

// A thread full of images replays only the newest maxHistoryImages of them, as thumbnails;
// with vision off it replays none (what-iff#419).
func TestBuildHistoryContext_CapsHistoryImages(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	var history []*models.ChatMessage
	for range 8 {
		history = append(history, &models.ChatMessage{
			Origin:      models.MessageOriginAssistant,
			Message:     "here it is",
			Attachments: []*models.FileAttachment{{ID: uuid.New(), Name: "gen.png", FileType: "image/png"}},
		})
	}
	b := testMessageContextBuilder(t)
	b.fileStore = historyImageStore{content: []byte("thumb")}
	b.loadHistoryOverride = func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, *time.Time, string) []*models.ChatMessage {
		return history
	}
	chat := &models.Chat{ID: uuid.New(), UserID: userID}

	countImages := func(m *provider.ModelContext) (turnsWithImages []int) {
		for i, seg := range m.Segments {
			if seg.Kind == provider.SegmentKindHistoryTurn && len(seg.UserImages) > 0 {
				turnsWithImages = append(turnsWithImages, i)
			}
		}
		return turnsWithImages
	}

	modelCtx, _, _, err := b.buildHistoryContext(t.Context(), userID, chat, nil, true)
	require.NoError(t, err)
	withImages := countImages(modelCtx)
	require.Len(t, withImages, maxHistoryImages)
	last := 0
	for i, seg := range modelCtx.Segments {
		if seg.Kind == provider.SegmentKindHistoryTurn {
			last = i
		}
	}
	require.Equal(t, last, withImages[len(withImages)-1], "the newest turns keep their images")

	modelCtx, _, _, err = b.buildHistoryContext(t.Context(), userID, chat, nil, false)
	require.NoError(t, err)
	require.Empty(t, countImages(modelCtx))
}

// One reply that generated more images than the cap keeps its newest ones: by CreatedAt, then by
// position. This is the shape of a turn that called generate_image several times (what-iff#419).
func TestSelectHistoryImages_OneMessageKeepsItsNewestImages(t *testing.T) {
	t.Parallel()

	msg, ids := imageMessage(10)
	got := selectHistoryImages(nil, []*models.ChatMessage{msg}, 5)
	require.Len(t, got, 5)
	for _, id := range ids[5:] {
		require.Contains(t, got, id)
	}

	// Timestamps win over position when they differ.
	base := time.Now()
	for i, att := range msg.Attachments[1:] {
		att.CreatedAt = base.Add(-time.Duration(i) * time.Minute) // the first image is the newest
	}
	got = selectHistoryImages(nil, []*models.ChatMessage{msg}, 2)
	require.Contains(t, got, ids[0])
	require.Contains(t, got, ids[1])
}
