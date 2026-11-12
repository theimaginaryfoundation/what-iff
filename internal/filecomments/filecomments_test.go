package filecomments

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type memFiles map[string][]byte

func (m memFiles) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	m[key] = content
	return nil
}
func (m memFiles) DownloadFile(_ context.Context, key string) ([]byte, error) { return m[key], nil }
func (m memFiles) DeleteFile(_ context.Context, key string) error {
	delete(m, key)
	return nil
}

type fakeStore struct {
	file *models.FileAttachment
	// copies are other rows of the same file (chat copies), by id.
	copies map[uuid.UUID]*models.FileAttachment
	set    *models.FileCommentSet
	setFor uuid.UUID
	puts   int
	// afterGet, when set, runs once after the next GetFileAttachment: a save landing mid-request.
	afterGet func()
}

func (f *fakeStore) GetFileAttachment(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.FileAttachment, error) {
	row := f.file
	if c, ok := f.copies[id]; ok {
		row = c
	}
	copied := *row
	if after := f.afterGet; after != nil {
		f.afterGet = nil
		after()
	}
	return &copied, nil
}

func (f *fakeStore) CommentFileID(_ context.Context, _ uuid.UUID, id uuid.UUID) (uuid.UUID, error) {
	if c, ok := f.copies[id]; ok && c.OriginalID != nil {
		return *c.OriginalID, nil
	}
	return id, nil
}
func (f *fakeStore) GetFileCommentSet(_ context.Context, _ uuid.UUID, fileID uuid.UUID) (*models.FileCommentSet, error) {
	if f.set == nil || fileID != f.setFor {
		return nil, nil
	}
	copied := *f.set
	return &copied, nil
}
func (f *fakeStore) PutFileCommentSet(_ context.Context, _, fileID uuid.UUID, set models.FileCommentSet, expect int) (int, error) {
	if f.set != nil && fileID != f.setFor {
		return 0, errors.New("a second comment set for one file")
	}
	f.setFor = fileID
	current := 0
	if f.set != nil {
		current = f.set.Revision
	}
	if current != expect {
		return 0, ErrRevision
	}
	f.puts++
	set.Revision = current + 1
	f.set = &set
	return set.Revision, nil
}

func setup(text string) (*Service, *fakeStore, memFiles) {
	files := memFiles{"k1": []byte(text)}
	store := &fakeStore{file: &models.FileAttachment{ID: uuid.New(), S3Key: "k1", Name: "notes.txt", FileType: "text/plain", ContentVersion: 1}}
	return New(store, files), store, files
}

func thread(id string, start, end int) models.FileCommentThread {
	return models.FileCommentThread{
		ID:       id,
		Anchor:   &models.FileCommentRange{Start: start, End: end},
		Comments: []models.FileComment{{ID: id, AuthorID: "user:u1", AuthorName: "Pat", Text: "Hmm"}},
	}
}

const line = "This is an inline comment in a text file"

func TestWritePlacesThreadsOnTheCurrentTextAndQuotesThem(t *testing.T) {
	svc, store, _ := setup(line)
	ctx := context.Background()
	got, err := svc.Write(ctx, uuid.New(), store.file.ID, []models.FileCommentThread{thread("a", 11, 25), thread("bad", 30, 999)}, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || got.Threads[0].Quote != "inline comment" || got.Threads[1].Anchor != nil {
		t.Fatalf("got %+v", got)
	}
	if store.set.BaselineText != line || store.set.BaselineKey != "k1" {
		t.Fatalf("baseline %+v", store.set)
	}
	// A write against a version that is no longer the file's, or a stale revision, is refused.
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, nil, 2, 1); !errors.Is(err, ErrStaleContent) {
		t.Fatalf("stale content: %v", err)
	}
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, nil, 1, 0); !errors.Is(err, ErrRevision) {
		t.Fatalf("stale revision: %v", err)
	}
}

func TestReadCarriesAnchorsToTextChangedElsewhere(t *testing.T) {
	svc, store, files := setup(line)
	ctx := context.Background()
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, []models.FileCommentThread{thread("whole", 0, len(line)), thread("words", 11, 25)}, 1, 0); err != nil {
		t.Fatal(err)
	}
	// An agent rewrites the file.
	files["k2"] = []byte("I've updated this inline comment in Go source code")
	store.file.S3Key, store.file.ContentVersion = "k2", 2

	got, err := svc.Read(ctx, uuid.New(), store.file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentVersion != 2 || got.Revision != 2 {
		t.Fatalf("got %+v", got)
	}
	if got.Threads[0].Quote != "inline comment in" || got.Threads[1].Quote != "inline comment" {
		t.Fatalf("quotes %q %q", got.Threads[0].Quote, got.Threads[1].Quote)
	}
	// The carried anchors are stored, so the next read is direct.
	puts := store.puts
	if _, err := svc.Read(ctx, uuid.New(), store.file.ID); err != nil || store.puts != puts {
		t.Fatalf("second read wrote again: %v", err)
	}

	// All of a thread's words gone: it is on the whole file, with the quote it had.
	files["k3"] = []byte("Something else entirely")
	store.file.S3Key, store.file.ContentVersion = "k3", 3
	got, err = svc.Read(ctx, uuid.New(), store.file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Threads[1].Anchor != nil || got.Threads[1].Quote != "inline comment" {
		t.Fatalf("detached %+v", got.Threads[1])
	}
}

func TestAMissingObjectMovesNoAnchor(t *testing.T) {
	svc, store, files := setup(line)
	ctx := context.Background()
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, []models.FileCommentThread{thread("words", 11, 25)}, 1, 0); err != nil {
		t.Fatal(err)
	}
	edited := []byte("Now: " + line)

	// The row is on k2 when a read loads it; a save then swaps it to k3 and releases k2 before the
	// read reaches the object.
	files["k2"], files["k3"] = []byte("Then: "+line), edited
	store.file.S3Key, store.file.ContentVersion = "k2", 2
	store.afterGet = func() {
		delete(files, "k2")
		store.file.S3Key, store.file.ContentVersion = "k3", 3
	}
	got, err := svc.Read(ctx, uuid.New(), store.file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Threads[0].Anchor == nil || got.Threads[0].Anchor.Start != 16 {
		t.Fatalf("the read did not retry on the new object: %+v", got.Threads[0])
	}

	// An object that is simply gone is an error, and nothing is stored over the baseline.
	store.file.S3Key = "gone"
	puts := store.puts
	if _, err := svc.Read(ctx, uuid.New(), store.file.ID); !errors.Is(err, ErrContentMissing) || store.puts != puts {
		t.Fatalf("got %v, puts %d→%d", err, puts, store.puts)
	}
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, nil, 3, got.Revision); !errors.Is(err, ErrContentMissing) {
		t.Fatalf("write: %v", err)
	}
	// A write that lost the race to a save is told the content changed, so it re-reads.
	store.afterGet = func() { store.file.ContentVersion = 4 }
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, nil, 3, got.Revision); !errors.Is(err, ErrStaleContent) {
		t.Fatalf("raced write: %v", err)
	}

	// A file that stopped being text keeps its comments readable, on the whole file.
	files["binary"] = []byte{0xff, 0xfe, 0x00}
	store.file.S3Key = "binary"
	puts = store.puts
	if got, err := svc.Read(ctx, uuid.New(), store.file.ID); err != nil || len(got.Threads) != 1 || got.Threads[0].Anchor != nil || store.puts != puts {
		t.Fatalf("got %+v, %v", got, err)
	}

	// An empty file is text, not a missing one.
	files["empty"] = []byte{}
	store.file.S3Key, store.file.ContentVersion = "empty", 5
	if _, err := svc.Read(ctx, uuid.New(), store.file.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReadOfAFileWithNoCommentsAndOfBinary(t *testing.T) {
	svc, store, files := setup(line)
	got, err := svc.Read(context.Background(), uuid.New(), store.file.ID)
	if err != nil || len(got.Threads) != 0 || got.Revision != 0 || got.ContentVersion != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	files["k1"] = []byte{0xff, 0xfe, 0x00}
	if _, err := svc.Write(context.Background(), uuid.New(), store.file.ID, nil, 1, 0); !errors.Is(err, ErrNotText) {
		t.Fatalf("binary: %v", err)
	}
}

func TestAFileAndItsChatCopyShareOneSetOfComments(t *testing.T) {
	svc, store, files := setup(line)
	ctx := context.Background()
	original := store.file.ID
	// A chat copy: its own row (and version count), the same stored object.
	copyID := uuid.New()
	store.copies = map[uuid.UUID]*models.FileAttachment{copyID: {ID: copyID, S3Key: "k1", Name: "notes.txt", FileType: "text/plain", ContentVersion: 1, OriginalID: &original}}

	// The user comments on the copy (from the chat); the set is the original's.
	if _, err := svc.Write(ctx, uuid.New(), copyID, []models.FileCommentThread{thread("t", 11, 25)}, 1, 0); err != nil {
		t.Fatal(err)
	}
	if store.setFor != original {
		t.Fatalf("stored for %v, want the original", store.setFor)
	}
	got, err := svc.Read(ctx, uuid.New(), original)
	if err != nil || len(got.Threads) != 1 || got.Threads[0].Quote != "inline comment" {
		t.Fatalf("read through the original: %+v, %v", got, err)
	}

	// The agent edits the file: both rows move to the new object, at their own versions.
	files["k2"] = []byte("I've updated this inline comment in Go source code")
	store.file.S3Key, store.file.ContentVersion = "k2", 2
	store.copies[copyID].S3Key, store.copies[copyID].ContentVersion = "k2", 2
	got, err = svc.Read(ctx, uuid.New(), copyID)
	if err != nil || got.Threads[0].Quote != "inline comment" || got.ContentVersion != 2 {
		t.Fatalf("read through the copy after the edit: %+v, %v", got, err)
	}
	// The user replies from the gallery: placed on the original's version.
	got.Threads[0].Comments = append(got.Threads[0].Comments, models.FileComment{ID: "r", Text: "Thanks"})
	if _, err := svc.Write(ctx, uuid.New(), original, got.Threads, 2, got.Revision); err != nil {
		t.Fatal(err)
	}
	got, err = svc.Read(ctx, uuid.New(), copyID)
	if err != nil || len(got.Threads[0].Comments) != 2 {
		t.Fatalf("the reply through the copy: %+v, %v", got, err)
	}
}

func TestAnImagesThreadsAreOnTheWholeFileAndOutliveAnEdit(t *testing.T) {
	ctx := context.Background()
	files := memFiles{"img1": {0x89, 'P', 'N', 'G', 0xff, 0x00}}
	store := &fakeStore{file: &models.FileAttachment{ID: uuid.New(), S3Key: "img1", Name: "photo.png", FileType: "image/png", ContentVersion: 1}}
	svc := New(store, files)

	// An anchor sent for an image is dropped: its threads are on the whole file.
	got, err := svc.Write(ctx, uuid.New(), store.file.ID, []models.FileCommentThread{thread("t1", 0, 3)}, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Threads[0].Anchor != nil || got.Threads[0].Quote != "" || got.Revision != 1 {
		t.Fatalf("placed %+v", got)
	}
	if store.set.BaselineKey != "img1" || store.set.BaselineText != "" {
		t.Fatalf("stored %+v", store.set)
	}

	// The image is edited (a new object); its threads are read as they were, with nothing stored.
	files["img2"] = []byte{0x89, 'P', 'N', 'G', 0x01}
	store.file.S3Key, store.file.ContentVersion = "img2", 2
	got, err = svc.Read(ctx, uuid.New(), store.file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Threads) != 1 || got.Threads[0].ID != "t1" || got.ContentVersion != 2 || store.puts != 1 {
		t.Fatalf("read %+v after %d puts", got, store.puts)
	}

	// A write is still placed on the image's current content.
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, got.Threads, 1, got.Revision); !errors.Is(err, ErrStaleContent) {
		t.Fatalf("stale: %v", err)
	}
}

func TestWriteSetsAuthorshipOnTheServer(t *testing.T) {
	svc, store, _ := setup(line)
	ctx := context.Background()
	userID := uuid.New()
	me := "user:" + userID.String()
	agent := models.FileComment{ID: "a1", AuthorID: "personality:p1", AuthorName: "Vix", CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Text: "Agent says"}
	store.set = &models.FileCommentSet{Threads: []models.FileCommentThread{{ID: "t", Comments: []models.FileComment{agent}}}, BaselineKey: "k1", BaselineText: line, Revision: 1}
	store.setFor = store.file.ID

	future := time.Now().Add(time.Hour)
	forged := models.FileComment{ID: "a1", AuthorID: me, AuthorName: "Pat", Text: "Reworded"}
	mine := models.FileComment{ID: "c2", AuthorID: "personality:p1", AuthorName: "Pat", CreatedAt: future, Text: "Mine"}
	got, err := svc.Write(ctx, userID, store.file.ID, []models.FileCommentThread{{ID: "t", Comments: []models.FileComment{forged, mine}}}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	cs := got.Threads[0].Comments
	if cs[0].AuthorID != "personality:p1" || cs[0].Text != "Agent says" || !cs[0].CreatedAt.Equal(agent.CreatedAt) {
		t.Fatalf("the agent's comment changed: %+v", cs[0])
	}
	if cs[1].AuthorID != me || cs[1].CreatedAt.After(time.Now()) {
		t.Fatalf("a new comment is the user's, dated by the server: %+v", cs[1])
	}
}
