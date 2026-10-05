package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Issue #268: the history window after a checkpoint starts just after the checkpointed reply, so a
// message sent while the checkpoint ran is not hidden behind a summary that never saw it.

func TestCheckpointWindowStartAfter(t *testing.T) {
	t.Parallel()
	user := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	reply := user.Add(20 * time.Second)
	afterReply := reply.Add(time.Microsecond)
	beforeUser := user.Add(-time.Microsecond)
	at := func(d time.Duration) *time.Time { v := user.Add(d); return &v }

	for name, tc := range map[string]struct {
		user       time.Time
		firstOther *time.Time
		want       time.Time
	}{
		"nothing else since the user message":        {user, nil, afterReply},
		"next message sent during the checkpoint":    {user, at(25 * time.Second), afterReply},
		"a message arrived mid-inference":            {user, at(5 * time.Second), beforeUser},
		"a message saved in the reply's microsecond": {user, at(20 * time.Second), beforeUser},
		"no user message time (lookup was not made)": {time.Time{}, at(5 * time.Second), afterReply},
		"a message one microsecond after the reply":  {user, &afterReply, afterReply},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, checkpointWindowStartAfter(tc.user, reply, tc.firstOther))
		})
	}
}

func TestCheckpointWindowStart_FallsBackToNowWithoutAReplyTime(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()} // no datastore: the lookup must not run
	for name, reply := range map[string]*models.ChatMessage{
		"no reply":          nil,
		"reply has no time": {ID: uuid.New()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			before := time.Now()
			got := a.checkpointWindowStart(context.Background(), uuid.New(), uuid.New(), &models.ChatMessage{SentAt: before}, reply)
			require.False(t, got.Before(before))
			require.False(t, got.After(time.Now()))
		})
	}
}

func TestCheckpointWindowStart_NoUserMessageTimeSkipsTheLookup(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()} // no datastore: the lookup must not run
	reply := time.Now().Add(-time.Minute)
	got := a.checkpointWindowStart(context.Background(), uuid.New(), uuid.New(), nil, &models.ChatMessage{ID: uuid.New(), SentAt: reply})
	require.Equal(t, reply.Add(time.Microsecond), got)
}

func TestCheckpointWindowStart_LooksForMessagesTheSummaryDidNotSee(t *testing.T) {
	t.Parallel()
	user := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	userMsg := &models.ChatMessage{ID: uuid.New(), SentAt: user}
	agentMsg := &models.ChatMessage{ID: uuid.New(), SentAt: user.Add(20 * time.Second)}

	t.Run("one arrived mid-turn", func(t *testing.T) {
		t.Parallel()
		ds, mock, cleanup := newTestDatastore(t)
		defer cleanup()
		mock.ExpectQuery("SELECT .*sent_at.* FROM `chat_messages`").
			WillReturnRows(sqlmock.NewRows([]string{"sent_at"}).AddRow(user.Add(5 * time.Second)))
		a := &Agent{ds: ds, logger: zap.NewNop()}

		got := a.checkpointWindowStart(context.Background(), uuid.New(), uuid.New(), userMsg, agentMsg)
		require.Equal(t, user.Add(-time.Microsecond), got, "the whole turn stays live with the unseen message")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("none", func(t *testing.T) {
		t.Parallel()
		ds, mock, cleanup := newTestDatastore(t)
		defer cleanup()
		mock.ExpectQuery("SELECT .*sent_at.* FROM `chat_messages`").
			WillReturnRows(sqlmock.NewRows([]string{"sent_at"}))
		a := &Agent{ds: ds, logger: zap.NewNop()}

		got := a.checkpointWindowStart(context.Background(), uuid.New(), uuid.New(), userMsg, agentMsg)
		require.Equal(t, agentMsg.SentAt.Add(time.Microsecond), got)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("lookup fails", func(t *testing.T) {
		t.Parallel()
		ds, mock, cleanup := newTestDatastore(t)
		defer cleanup()
		mock.ExpectQuery("SELECT .*sent_at.* FROM `chat_messages`").WillReturnError(errors.New("db hiccup"))
		a := &Agent{ds: ds, logger: zap.NewNop()}

		got := a.checkpointWindowStart(context.Background(), uuid.New(), uuid.New(), userMsg, agentMsg)
		require.Equal(t, agentMsg.SentAt.Add(time.Microsecond), got, "still after the reply, never now")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCheckpointWindowStart_NoDatastoreSkipsTheLookup(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()} // no datastore: the lookup must not run, even with a user message time
	reply := time.Now().Add(-time.Minute)
	userMsg := &models.ChatMessage{ID: uuid.New(), SentAt: reply.Add(-time.Second)}
	got := a.checkpointWindowStart(context.Background(), uuid.New(), uuid.New(), userMsg, &models.ChatMessage{ID: uuid.New(), SentAt: reply})
	require.Equal(t, reply.Add(time.Microsecond), got)
}
