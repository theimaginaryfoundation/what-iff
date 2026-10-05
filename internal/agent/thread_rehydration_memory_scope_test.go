package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

// newImportMemoryScopeAgent builds an Agent whose memory extraction (Responses API) and embeddings
// are served by one httptest server and whose datastore is a sqlmock. Expected SQL is a regexp; a
// "NOT " prefix means the statement must NOT match it, so a test can assert that a memory INSERT
// omits pinned_personality_id.
func newImportMemoryScopeAgent(t *testing.T, extracted string) (*Agent, sqlmock.Sqlmock) {
	t.Helper()
	matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		if rest, ok := strings.CutPrefix(expected, "NOT "); ok {
			if regexp.MustCompile(rest).MatchString(actual) {
				return fmt.Errorf("sql %q unexpectedly matches %q", actual, rest)
			}
			return nil
		}
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	t.Cleanup(func() { _ = client.Close() })
	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "embeddings") {
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"m","usage":{"prompt_tokens":1,"total_tokens":1}}`))
			return
		}
		_, _ = w.Write([]byte(responseTextJSONBody("resp_scope", extracted)))
	}))
	t.Cleanup(srv.Close)

	oai := openai.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("test-key"), option.WithMaxRetries(0))
	a := &Agent{
		ds:             ds,
		logger:         zap.NewNop(),
		OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL),
		memoryTool:     tools.NewVectorStoreMemoryTool(ds, &oai, zap.NewNop()),
	}
	return a, mock
}

// Rehydrating an imported thread seeds memories under the same auto-pin rule as every other memory
// path, using the personality the thread has when rehydration runs: a User-scoped (global) memory is
// pinned to that personality exactly when it has auto_pin_memories on, a Chat-scoped memory never
// is, and a thread with no personality (or one whose lookup fails) stays unpinned.
func TestExtractAndStoreImportedMemories_AppliesThreadPersonalityAutoPin(t *testing.T) {
	t.Parallel()
	const extracted = `{\"memories\":[` +
		`{\"content\":\"Prefers oolong\",\"scope\":\"User\",\"confidence\":\"high\"},` +
		`{\"content\":\"Project is called Kettle\",\"scope\":\"Chat\",\"confidence\":\"high\"}]}`

	cases := []struct {
		name        string
		personality bool // thread has an assigned personality
		autoPin     bool
		lookupFails bool
		wantPinned  bool // the User-scoped memory is pinned to the personality
	}{
		{name: "personality with pin memories on", personality: true, autoPin: true, wantPinned: true},
		{name: "personality with pin memories off", personality: true, autoPin: false},
		{name: "no personality assigned"},
		{name: "personality lookup fails", lookupFails: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, mock := newImportMemoryScopeAgent(t, extracted)
			userID, chatID, personalityID := uuid.New(), uuid.New(), uuid.New()

			// Rehydration-time personality lookup.
			lookup := mock.ExpectQuery("SELECT .* FROM `personalities`")
			switch {
			case tc.lookupFails:
				lookup.WillReturnError(errCoverageTestSentinel)
			case tc.personality:
				lookup.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(personalityID))
			default:
				lookup.WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}

			// User-scoped memory: consults the personality's auto-pin flag only when one is active.
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .* FROM `chats`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(chatID))
			if tc.personality {
				mock.ExpectQuery("SELECT .* FROM `personalities`").
					WithArgs(personalityID).
					WillReturnRows(sqlmock.NewRows([]string{"id", "auto_pin_memories"}).AddRow(personalityID, tc.autoPin))
			}
			if tc.wantPinned {
				mock.ExpectExec("INSERT INTO `memories` .*`pinned_personality_id`").WillReturnResult(sqlmock.NewResult(1, 1))
			} else {
				mock.ExpectExec("NOT pinned_personality_id").WillReturnResult(sqlmock.NewResult(1, 1))
			}
			mock.ExpectExec("INSERT INTO `embeddings`").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()

			// Chat-scoped memory: never pinned, so the personality is not even consulted.
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .* FROM `chats`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(chatID))
			mock.ExpectExec("NOT pinned_personality_id").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("INSERT INTO `embeddings`").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()

			a.extractAndStoreImportedMemories(context.Background(), userID, chatID, turnMsgs(2))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
