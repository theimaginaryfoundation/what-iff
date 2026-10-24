package scheduler

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// threadRecordingStore records the thread a job is given when it has none.
type threadRecordingStore struct {
	fakeDatastoreProvider
	created []models.Chat
	linked  *uuid.UUID
}

func (s *threadRecordingStore) CreateChat(_ context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error) {
	s.created = append(s.created, chat)
	chat.ID, chat.UserID = uuid.New(), userID
	return &chat, nil
}

func (s *threadRecordingStore) SetAgentJobChat(_ context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error) {
	s.linked = chatID
	return &models.AgentJob{ID: id, UserID: userID, ChatID: chatID}, nil
}

// A job set to run in a thread (by the app, or by an agent's thread_id) runs there; no thread is
// made for it.
func TestResolveAgentJobChatUsesTheJobsThread(t *testing.T) {
	store := &threadRecordingStore{}
	m := &Manager{ds: store, logger: zap.NewNop()}
	thread := uuid.New()
	job := &models.AgentJob{ID: uuid.New(), UserID: uuid.New(), ChatID: &thread}

	got, _, err := m.resolveAgentJobChat(context.Background(), job.UserID, job.ID, job)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, thread, *got)
	assert.Empty(t, store.created)
	assert.Nil(t, store.linked)
}

// A job without one (created with thread_id "new", or whose thread was deleted) gets a dedicated
// thread on its run, which it keeps.
func TestResolveAgentJobChatMakesAThreadForAJobWithoutOne(t *testing.T) {
	store := &threadRecordingStore{}
	m := &Manager{ds: store, logger: zap.NewNop()}
	title := "Morning check-in"
	job := &models.AgentJob{ID: uuid.New(), UserID: uuid.New(), Title: &title}

	got, updated, err := m.resolveAgentJobChat(context.Background(), job.UserID, job.ID, job)

	require.NoError(t, err)
	require.Len(t, store.created, 1)
	assert.Equal(t, "[JOB]: Morning check-in", store.created[0].Name)
	require.NotNil(t, store.linked)
	assert.Equal(t, *store.linked, *got)
	assert.Equal(t, *got, *updated.ChatID)
}
