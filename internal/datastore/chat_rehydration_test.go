package datastore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestClaimChatRehydration_ClaimsOnlyAQualifyingThread(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	// The conditions live in the UPDATE itself, so the claim is atomic: unarchived, imported, no
	// summary yet, and not pending/processing/ready.
	mock.ExpectExec("UPDATE .chats. SET .updated_at. = \\?, .rehydration_state. = \\? WHERE "+
		".*"+
		".chats.\\..source. IS NOT NULL.*.chats.\\..source. <> \\?.*"+
		".chats.\\..checkpoint_summary. IS NULL OR .chats.\\..checkpoint_summary. = \\?.*"+
		".chats.\\..rehydration_state. IS NULL OR .chats.\\..rehydration_state. IN \\(\\?, \\?\\).*NOT .chats.\\..archived.$").
		WithArgs(sqlmock.AnyArg(), "pending", sqlmock.AnyArg(), sqlmock.AnyArg(), "", "", "", "failed").
		WillReturnResult(sqlmock.NewResult(0, 1))

	claimed, err := ds.ClaimChatRehydration(context.Background(), uuid.New(), uuid.New(), false)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimChatRehydration_NotClaimedWhenNothingMatches(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE .*chats.*`).WillReturnResult(sqlmock.NewResult(0, 0))

	claimed, err := ds.ClaimChatRehydration(context.Background(), uuid.New(), uuid.New(), false)
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimChatRehydration_ReturnsUpdateError(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE .*chats.*`).WillReturnError(context.DeadlineExceeded)

	claimed, err := ds.ClaimChatRehydration(context.Background(), uuid.New(), uuid.New(), false)
	require.Error(t, err)
	require.False(t, claimed)
}

// A turn into an archived thread is intent to use it, so the claim drops the unarchived condition.
func TestClaimChatRehydration_IncludeArchivedDropsTheArchivedCondition(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	// Without the archived condition the statement ends at the state check.
	mock.ExpectExec(`UPDATE .*chats.* WHERE .*IN \(\?, \?\)\)$`).
		WithArgs(sqlmock.AnyArg(), "pending", sqlmock.AnyArg(), sqlmock.AnyArg(), "", "", "", "failed").
		WillReturnResult(sqlmock.NewResult(0, 1))

	claimed, err := ds.ClaimChatRehydration(context.Background(), uuid.New(), uuid.New(), true)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetChatRehydrationInfo(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT .*rehydration_state.*source.*checkpoint_summary.* FROM .chats.`).
		WillReturnRows(sqlmock.NewRows([]string{"rehydration_state", "source", "checkpoint_summary"}).AddRow("", "openai", ""))

	info, err := ds.GetChatRehydrationInfo(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, ChatRehydrationInfo{State: "", Imported: true, Summarized: false}, info)
	require.True(t, info.NeedsRehydration())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetChatRehydrationInfo_NotFound(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT .*`).
		WillReturnRows(sqlmock.NewRows([]string{"rehydration_state", "source", "checkpoint_summary"}))

	_, err := ds.GetChatRehydrationInfo(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestChatRehydrationInfo_NeedsRehydration(t *testing.T) {
	cases := []struct {
		name string
		info ChatRehydrationInfo
		want bool
	}{
		{"imported, never summarized", ChatRehydrationInfo{Imported: true}, true},
		{"previous attempt failed", ChatRehydrationInfo{Imported: true, State: "failed"}, true},
		{"native thread", ChatRehydrationInfo{}, false},
		{"already summarized", ChatRehydrationInfo{Imported: true, Summarized: true}, false},
		{"pending", ChatRehydrationInfo{Imported: true, State: "pending"}, false},
		{"processing", ChatRehydrationInfo{Imported: true, State: "processing"}, false},
		{"ready (short thread, no summary needed)", ChatRehydrationInfo{Imported: true, State: "ready"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.info.NeedsRehydration())
		})
	}
}

func TestGetChatPersonalityID(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	personalityID := uuid.New()
	mock.ExpectQuery("SELECT .* FROM .personalities.").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(personalityID))

	got, err := ds.GetChatPersonalityID(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, personalityID, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// A chat with no personality (or one the user does not own) is not an error: it just has none.
func TestGetChatPersonalityID_NoneAssigned(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .* FROM .personalities.").WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := ds.GetChatPersonalityID(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, got)
}

func TestGetChatPersonalityID_ReturnsQueryError(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .* FROM .personalities.").WillReturnError(sql.ErrConnDone)

	_, err := ds.GetChatPersonalityID(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, sql.ErrConnDone)
}
