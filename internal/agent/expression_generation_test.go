package agent

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestBuildExpressionPickCatalog_alwaysIncludesLabels(t *testing.T) {
	t.Parallel()
	slots := []models.PersonalityExpression{
		{ExpressionKey: "happy", Label: strPtr("warm and friendly")},
	}
	cat := buildExpressionPickCatalog(slots)
	require.Contains(t, cat, "- happy — warm and friendly")
}

func TestBuildExpressionPickUserMessageBody(t *testing.T) {
	t.Parallel()
	body := buildExpressionPickUserMessageBody("- happy — warm")
	require.Contains(t, body, "Classify the latest assistant reply")
	require.Contains(t, body, "expression_key")
	require.Contains(t, body, "- happy — warm")
	require.Contains(t, body, "MUST NOT invent")
	require.NotContains(t, body, "focused, etc.")
}

func TestParseExpressionPickPayload(t *testing.T) {
	t.Parallel()
	k, r := parseExpressionPickPayload(`{"expression_key":"sad","reasoning":"  Tone was subdued. "}`)
	require.Equal(t, "sad", k)
	require.Equal(t, "Tone was subdued.", r)
}

func TestParseExpressionPickPayload_invalidJSON(t *testing.T) {
	t.Parallel()
	k, r := parseExpressionPickPayload("not json")
	require.Empty(t, k)
	require.Empty(t, r)
}

func strPtr(s string) *string { return &s }

// --- expressionPickStructuredOutputFormat ---

func TestExpressionPickStructuredOutputFormat(t *testing.T) {
	t.Parallel()
	out := expressionPickStructuredOutputFormat()
	require.NotNil(t, out.Format.OfJSONSchema)
	require.Equal(t, "ExpressionPortraitPick", out.Format.OfJSONSchema.Name)
}

// --- PickGenerationExpression ---

func TestPickGenerationExpression_ListExpressionsErrorIsWrapped(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	// ensureUserOwnsPersonality's exists-check fails, so ListPersonalityExpressions returns
	// ErrPersonalityNotFound before ever reaching the personality_expression query.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	id, reasoning, err := a.PickGenerationExpression(context.Background(), uuid.New(), uuid.New(), nil, "hi", "hello there")
	require.Error(t, err)
	require.ErrorContains(t, err, "list expressions")
	require.Nil(t, id)
	require.Empty(t, reasoning)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPickGenerationExpression_NoSlotsReturnsNilWithoutError(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	// SQLite's Exist() lowers to "SELECT id ... LIMIT 1" rather than a boolean EXISTS(...)
	// projection, so a matching row (any id) signals the personality is owned.
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "expression_key"}))
	mock.ExpectCommit()

	a := newTestAgent(ds)
	id, reasoning, err := a.PickGenerationExpression(context.Background(), uuid.New(), uuid.New(), nil, "hi", "hello there")
	require.NoError(t, err)
	require.Nil(t, id)
	require.Empty(t, reasoning)
	require.NoError(t, mock.ExpectationsWereMet())
}

func personalityExpressionRows(key string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "created_at", "updated_at", "expression_key", "label", "personality_expressions", "personality_expression_image"}).
		AddRow(uuid.New().String(), time.Now(), time.Now(), key, nil, uuid.New().String(), nil)
}

// TestPickGenerationExpression_StandaloneSuccessMatchesKey exercises the nil-inferenceModelCtx
// (standalone prompt) branch end to end: ListPersonalityExpressions succeeds with one slot, the
// OpenAI call returns a structured pick matching that slot's key, and the returned id/reasoning
// come from the matched slot.
func TestPickGenerationExpression_StandaloneSuccessMatchesKey(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(personalityExpressionRows("happy"))
	mock.ExpectCommit()

	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", `{\"expression_key\":\"happy\",\"reasoning\":\"Warm tone.\"}`))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), ds: ds, OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	id, reasoning, err := a.PickGenerationExpression(context.Background(), uuid.New(), uuid.New(), nil, "hi", "hello there")
	require.NoError(t, err)
	require.NotNil(t, id)
	require.Equal(t, "Warm tone.", reasoning)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestPickGenerationExpression_UnknownKeyReturnsNil covers a classifier pick that doesn't match
// any configured slot: no id, no error, just a debug log.
func TestPickGenerationExpression_UnknownKeyReturnsNil(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(personalityExpressionRows("happy"))
	mock.ExpectCommit()

	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", `{\"expression_key\":\"nonexistent\",\"reasoning\":\"n/a\"}`))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), ds: ds, OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	id, reasoning, err := a.PickGenerationExpression(context.Background(), uuid.New(), uuid.New(), nil, "hi", "hello there")
	require.NoError(t, err)
	require.Nil(t, id)
	require.Empty(t, reasoning)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestPickGenerationExpression_ProviderErrorReturnsNilWithoutError documents that a classifier
// call failure is swallowed (logged) rather than propagated — callers get "no portrait" instead
// of an error.
func TestPickGenerationExpression_ProviderErrorReturnsNilWithoutError(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(personalityExpressionRows("happy"))
	mock.ExpectCommit()

	a := &Agent{logger: zap.NewNop(), ds: ds, OpenAIProvider: newHTTPTestOpenAIProvider("http://127.0.0.1:0")}
	id, reasoning, err := a.PickGenerationExpression(context.Background(), uuid.New(), uuid.New(), nil, "hi", "hello there")
	require.NoError(t, err)
	require.Nil(t, id)
	require.Empty(t, reasoning)
	require.NoError(t, mock.ExpectationsWereMet())
}
