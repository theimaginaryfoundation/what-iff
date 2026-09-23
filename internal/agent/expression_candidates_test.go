package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// expectPersonalityLookup mocks GetPersonality returning one personality with the given image
// style and no moods/expressions (the eager-loaded edge queries come back empty).
func expectPersonalityLookup(mock sqlmock.Sqlmock, personalityID uuid.UUID, imageStyle string) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `personalities`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at", "name", "system_prompt", "image_style"}).
			AddRow(personalityID.String(), time.Now(), time.Now(), "Vix", "You are Vix.", imageStyle))
	mock.ExpectQuery("SELECT .* FROM `personality_expressions`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT .* FROM `moods`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()
}

// expectFileAttachmentLookup mocks GetFileAttachment returning an owned attachment of fileType
// that no expression slot references.
func expectFileAttachmentLookup(mock sqlmock.Sqlmock, attID uuid.UUID, fileType string) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `file_attachments`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at", "name", "file_type"}).
			AddRow(attID.String(), time.Now(), time.Now(), "ref.png", fileType))
	// Expression slots currently showing this image (none).
	mock.ExpectQuery("SELECT .* FROM `personality_expressions`").WillReturnRows(sqlmock.NewRows([]string{"personality_expression_image"}))
	mock.ExpectCommit()
}

func TestEnqueueExpressionCandidatesJob_WrongKeyCount(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	job, err := newTestAgent(ds).EnqueueExpressionCandidatesJob(context.Background(), uuid.New(), uuid.New(), []string{"happy"}, nil)
	require.Nil(t, job)
	require.ErrorIs(t, err, ErrExpressionCandidateKeyCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnqueueExpressionCandidatesJob_PersonalityNotFound(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `personalities`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	job, err := newTestAgent(ds).EnqueueExpressionCandidatesJob(context.Background(), uuid.New(), uuid.New(), ExpressionGridKeys, nil)
	require.Nil(t, job)
	require.ErrorIs(t, err, datastore.ErrPersonalityNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnqueueExpressionCandidatesJob_ImageStyleNone(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	pid := uuid.New()
	expectPersonalityLookup(mock, pid, models.ImageStyleNone)

	job, err := newTestAgent(ds).EnqueueExpressionCandidatesJob(context.Background(), uuid.New(), pid, ExpressionGridKeys, nil)
	require.Nil(t, job)
	require.ErrorIs(t, err, ErrExpressionImagesDisabled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnqueueExpressionCandidatesJob_ReferenceImageChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expect  func(mock sqlmock.Sqlmock, refID uuid.UUID)
		wantErr error
		// wantNotFound is false when a datastore failure must NOT be reported as a missing image.
		wantNotFound bool
	}{
		{
			name: "missing attachment",
			expect: func(mock sqlmock.Sqlmock, _ uuid.UUID) {
				mock.ExpectBegin()
				mock.ExpectQuery("SELECT .* FROM `file_attachments`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
				mock.ExpectRollback()
			},
			wantNotFound: true,
		},
		{
			name: "attachment is not an image",
			expect: func(mock sqlmock.Sqlmock, refID uuid.UUID) {
				expectFileAttachmentLookup(mock, refID, "application/pdf")
			},
			wantNotFound: true,
		},
		{
			name: "datastore failure is not a missing image",
			expect: func(mock sqlmock.Sqlmock, _ uuid.UUID) {
				mock.ExpectBegin()
				mock.ExpectQuery("SELECT .* FROM `file_attachments`").WillReturnError(errors.New("connection reset"))
				mock.ExpectRollback()
			},
			wantErr: errors.New("connection reset"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds, mock, cleanup := newTestDatastore(t)
			defer cleanup()

			pid, refID := uuid.New(), uuid.New()
			expectPersonalityLookup(mock, pid, "auto")
			tc.expect(mock, refID)

			job, err := newTestAgent(ds).EnqueueExpressionCandidatesJob(context.Background(), uuid.New(), pid, ExpressionGridKeys, &refID)
			require.Nil(t, job)
			if tc.wantNotFound {
				require.ErrorIs(t, err, ErrExpressionReferenceImageNotFound)
			} else {
				require.NotErrorIs(t, err, ErrExpressionReferenceImageNotFound)
				require.ErrorContains(t, err, tc.wantErr.Error())
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestEnqueueExpressionCandidatesJob_ActiveJobBlocks(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	uid, pid, refID, activeID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	expectPersonalityLookup(mock, pid, "auto")
	expectFileAttachmentLookup(mock, refID, "image/PNG")
	mock.ExpectQuery("SELECT .* FROM `jobs`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at", "job_type", "status", "reference", "user_jobs"}).
			AddRow(activeID.String(), time.Now(), time.Now(), JobTypeExpressionGrid, "processing", pid.String(), uid.String()))
	mock.ExpectQuery("SELECT .* FROM `users`").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uid.String()))

	job, err := newTestAgent(ds).EnqueueExpressionCandidatesJob(context.Background(), uid, pid, ExpressionGridKeys, &refID)
	require.Nil(t, job)
	var active *ErrPersonalityMediaJobActive
	require.ErrorAs(t, err, &active)
	require.Equal(t, activeID, active.Job.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnqueueExpressionCandidatesJob_ActiveJobLookupFails(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	pid := uuid.New()
	expectPersonalityLookup(mock, pid, "auto")
	mock.ExpectQuery("SELECT .* FROM `jobs`").WillReturnError(errors.New("db down"))

	job, err := newTestAgent(ds).EnqueueExpressionCandidatesJob(context.Background(), uuid.New(), pid, ExpressionGridKeys, nil)
	require.Nil(t, job)
	require.ErrorContains(t, err, "find active personality media job")
	require.NoError(t, mock.ExpectationsWereMet())
}

// expectCreateJob mocks CreateJob (insert + owner reload) returning a pending job with jobID.
func expectCreateJob(mock sqlmock.Sqlmock, uid, jobID uuid.UUID, progress string) {
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `jobs`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT .* FROM `jobs`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at", "job_type", "status", "progress", "user_jobs"}).
			AddRow(jobID.String(), time.Now(), time.Now(), JobTypeExpressionGrid, "pending", progress, uid.String()))
	mock.ExpectQuery("SELECT .* FROM `users`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uid.String()))
	mock.ExpectCommit()
}

func TestEnqueueExpressionCandidatesJob_CreatesTaggedJobAndStartsWorker(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	uid, pid, refID, jobID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	expectPersonalityLookup(mock, pid, "auto")
	expectFileAttachmentLookup(mock, refID, "image/jpeg")
	mock.ExpectQuery("SELECT .* FROM `jobs`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	expectCreateJob(mock, uid, jobID, `{"mode":"candidates"}`)
	// The background worker's first step (mark processing) fails, then so does marking it
	// failed; both are only logged, which ends the worker without touching the image API.
	expectJobStatusUpdateNotFound(mock)
	expectJobStatusUpdateNotFound(mock)

	ctx := context.WithValue(context.Background(), middleware.UserIDKey, uid)
	job, err := newTestAgent(ds).EnqueueExpressionCandidatesJob(ctx, uid, pid, customGridKeys, &refID)
	require.NoError(t, err)
	require.Equal(t, jobID, job.ID)

	require.Eventually(t, func() bool { return mock.ExpectationsWereMet() == nil }, 5*time.Second, 10*time.Millisecond)
}

func TestEnqueueExpressionCandidatesJob_MissingUserContext(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	uid, pid := uuid.New(), uuid.New()
	expectPersonalityLookup(mock, pid, "auto")
	mock.ExpectQuery("SELECT .* FROM `jobs`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	expectCreateJob(mock, uid, uuid.New(), `{"mode":"candidates"}`)

	job, err := newTestAgent(ds).EnqueueExpressionCandidatesJob(context.Background(), uid, pid, ExpressionGridKeys, nil)
	require.Nil(t, job)
	require.ErrorContains(t, err, "user ID not found in context")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestExpressionCandidatesProgress_RoundTrip(t *testing.T) {
	t.Parallel()
	refID, imgID := uuid.New(), uuid.New()
	raw, err := json.Marshal(ExpressionCandidatesProgress{
		Mode:             ExpressionCandidatesMode,
		Expressions:      []string{"smug"},
		ReferenceImageID: &refID,
		Candidates:       []ExpressionCandidate{{ExpressionKey: "smug", ImageID: imgID}},
	})
	require.NoError(t, err)
	require.True(t, IsExpressionCandidatesProgress(string(raw)))
	require.JSONEq(t, `{"mode":"candidates","expressions":["smug"],"reference_image_id":"`+refID.String()+`",
		"candidates":[{"expression_key":"smug","image_id":"`+imgID.String()+`"}]}`, string(raw))
}
