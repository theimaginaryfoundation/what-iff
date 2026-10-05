package server

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"
	"go.uber.org/zap"
)

type fakeAttachmentDatastore struct {
	mu        sync.Mutex
	chatErr   error
	createErr error
	created   []models.FileAttachment
	deleted   []uuid.UUID
	s3Keys    map[uuid.UUID]string
}

func (f *fakeAttachmentDatastore) GetChat(_ context.Context, _, _ uuid.UUID) (*models.Chat, error) {
	if f.chatErr != nil {
		return nil, f.chatErr
	}
	return &models.Chat{}, nil
}

func (f *fakeAttachmentDatastore) CreateFileAttachment(_ context.Context, _ uuid.UUID, fa models.FileAttachment) (*models.FileAttachment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	fa.ID = uuid.New()
	f.created = append(f.created, fa)
	return &fa, nil
}

func (f *fakeAttachmentDatastore) DeleteFileAttachment(_ context.Context, _, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeAttachmentDatastore) SetFileAttachmentS3Key(_ context.Context, _, id uuid.UUID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.s3Keys == nil {
		f.s3Keys = map[uuid.UUID]string{}
	}
	f.s3Keys[id] = key
	return nil
}

type fakeAttachmentProvider struct {
	uploadErr error
	uploaded  []byte
	attrs     map[string]string
	deleted   []string
}

func (p *fakeAttachmentProvider) UploadFileAttachment(_ context.Context, _ uuid.UUID, attrs map[string]string, file io.Reader, _ string, _ utils.FileTypeInfo) (string, error) {
	if p.uploadErr != nil {
		return "", p.uploadErr
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	p.uploaded, p.attrs = data, attrs
	return "file-123", nil
}

func (p *fakeAttachmentProvider) DeleteFileAttachment(_ context.Context, fileID string) error {
	p.deleted = append(p.deleted, fileID)
	return nil
}

type fakeAttachmentFiles struct {
	mu       sync.Mutex
	uploaded map[string][]byte
	err      error
}

func (s *fakeAttachmentFiles) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.uploaded == nil {
		s.uploaded = map[string][]byte{}
	}
	s.uploaded[key] = content
	return nil
}
func (s *fakeAttachmentFiles) DownloadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (s *fakeAttachmentFiles) DeleteFile(context.Context, string) error             { return nil }

var _ storage.FileStore = (*fakeAttachmentFiles)(nil)

func newTestIngester(ds *fakeAttachmentDatastore, p *fakeAttachmentProvider, files *fakeAttachmentFiles) agentAttachmentIngester {
	return agentAttachmentIngester{ds: ds, provider: p, files: files, logger: zap.NewNop()}
}

func validUpload() plugins.AttachmentUpload {
	return plugins.AttachmentUpload{UserID: uuid.New(), ChatID: uuid.New(), Name: "notes.txt", Data: []byte("hello")}
}

func TestAgentAttachmentIngesterRejectsAFileOverTheUploadLimitBeforeTouchingAnything(t *testing.T) {
	// A nil provider and datastore prove the size guard runs before they are consulted.
	up := validUpload()
	up.Data = make([]byte, handlerutils.MaxUploadBytes+1)

	_, err := agentAttachmentIngester{}.IngestAttachment(context.Background(), up)

	assert.ErrorIs(t, err, plugins.ErrAttachmentTooLarge)
}

func TestAgentAttachmentIngesterAcceptsAFileExactlyAtTheUploadLimit(t *testing.T) {
	// At the limit the size guard lets it through, so the next check (configuration) is what stops it.
	up := validUpload()
	up.Data = make([]byte, handlerutils.MaxUploadBytes)

	_, err := agentAttachmentIngester{}.IngestAttachment(context.Background(), up)

	assert.ErrorIs(t, err, errAttachmentNotConfigured)
}

func TestAgentAttachmentIngesterMapsAMissingChatToThePluginSentinel(t *testing.T) {
	ds := &fakeAttachmentDatastore{chatErr: datastore.ErrChatNotFound}

	_, err := newTestIngester(ds, &fakeAttachmentProvider{}, &fakeAttachmentFiles{}).IngestAttachment(context.Background(), validUpload())

	assert.ErrorIs(t, err, plugins.ErrAttachmentChatNotFound)
	assert.Empty(t, ds.created)
}

func TestAgentAttachmentIngesterDoesNotMistakeALookupFailureForAMissingChat(t *testing.T) {
	boom := errors.New("db down")
	ds := &fakeAttachmentDatastore{chatErr: boom}

	_, err := newTestIngester(ds, &fakeAttachmentProvider{}, &fakeAttachmentFiles{}).IngestAttachment(context.Background(), validUpload())

	assert.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, plugins.ErrAttachmentChatNotFound)
}

func TestAgentAttachmentIngesterSavesAFileToTheChat(t *testing.T) {
	ds, provider, files := &fakeAttachmentDatastore{}, &fakeAttachmentProvider{}, &fakeAttachmentFiles{}
	up := validUpload()

	created, err := newTestIngester(ds, provider, files).IngestAttachment(context.Background(), up)

	require.NoError(t, err)
	assert.Equal(t, "notes.txt", created.Name)
	assert.Equal(t, up.UserID, created.UserID)
	assert.Equal(t, []byte("hello"), provider.uploaded, "the provider got the plugin's bytes")
	assert.Equal(t, up.ChatID.String(), provider.attrs["chat_id"])
	key := storage.FileKeyForChat(up.UserID, up.ChatID, created.ID, "notes.txt")
	assert.Equal(t, []byte("hello"), files.uploaded[key], "archived under the chat")
	assert.Equal(t, key, ds.s3Keys[created.ID])
}

func TestAgentAttachmentIngesterRunsWithoutALogger(t *testing.T) {
	ds, provider, files := &fakeAttachmentDatastore{}, &fakeAttachmentProvider{}, &fakeAttachmentFiles{}
	i := newTestIngester(ds, provider, files)
	i.logger = nil

	_, err := i.IngestAttachment(context.Background(), validUpload())

	assert.NoError(t, err)
}

func TestAgentAttachmentIngesterReportsAnUnsupportedFileType(t *testing.T) {
	ds, provider := &fakeAttachmentDatastore{}, &fakeAttachmentProvider{}
	up := validUpload()
	up.Name = "binary.exe"

	_, err := newTestIngester(ds, provider, &fakeAttachmentFiles{}).IngestAttachment(context.Background(), up)

	assert.ErrorIs(t, err, plugins.ErrAttachmentUnsupported)
	assert.Empty(t, ds.created, "nothing is saved for a rejected file")
}

func TestAgentAttachmentIngesterSurfacesAProviderFailureAsAnInternalError(t *testing.T) {
	ds, provider := &fakeAttachmentDatastore{}, &fakeAttachmentProvider{uploadErr: errors.New("openai down")}

	_, err := newTestIngester(ds, provider, &fakeAttachmentFiles{}).IngestAttachment(context.Background(), validUpload())

	require.Error(t, err)
	assert.NotErrorIs(t, err, plugins.ErrAttachmentUnsupported)
	assert.NotErrorIs(t, err, plugins.ErrAttachmentTooLarge)
	assert.Empty(t, ds.created)
}

func TestAgentAttachmentIngesterRollsBackWhenTheObjectStoreFails(t *testing.T) {
	ds, provider := &fakeAttachmentDatastore{}, &fakeAttachmentProvider{}
	files := &fakeAttachmentFiles{err: errors.New("s3 down")}

	_, err := newTestIngester(ds, provider, files).IngestAttachment(context.Background(), validUpload())

	require.Error(t, err)
	assert.Len(t, ds.deleted, 1, "the record is removed")
	assert.Equal(t, []string{"file-123"}, provider.deleted, "so is the provider's copy")
}

func TestNewAgentAttachmentIngesterLeavesAnUnconfiguredIngesterNotConfigured(t *testing.T) {
	// No agent, or an agent without a provider, must not become a non-nil provider interface.
	for name, i := range map[string]agentAttachmentIngester{
		"no agent":    newAgentAttachmentIngester(nil, nil),
		"no provider": newAgentAttachmentIngester(&agent.Agent{}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, i.provider)
			assert.Nil(t, i.ds)
			_, err := i.IngestAttachment(context.Background(), validUpload())
			assert.ErrorIs(t, err, errAttachmentNotConfigured)
		})
	}
}

func TestNewAgentAttachmentIngesterWiresTheAgentsProviderAndTheDatastore(t *testing.T) {
	prov, ds := &provider.OpenAIProvider{}, &datastore.Datastore{}

	i := newAgentAttachmentIngester(&agent.Agent{OpenAIProvider: prov}, ds)

	assert.Same(t, prov, i.provider)
	assert.Same(t, ds, i.ds)
}
