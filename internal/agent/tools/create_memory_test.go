package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// CreateMemoryTool's cheapest branch is the invalid-JSON-args guard, which
// fires before any embedding call or datastore access, so ds/oaiClient can
// stay nil.
func TestCreateMemoryTool_InvalidArgsReturnsErrorResult(t *testing.T) {
	t.Parallel()

	tool := &VectorStoreMemoryTool{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}

	out, err := tool.CreateMemoryTool(context.Background(), chat, []byte(`not json`))
	require.NoError(t, err)
	require.Contains(t, out, "invalid arguments")
	require.Contains(t, out, `"success":false`)
}

// newCreateMemoryTestTool wires CreateMemoryTool to a sqlmock-backed datastore and an embeddings
// stub. Expected SQL is a regexp; a "NOT " prefix means the statement must NOT match it, so a test
// can assert the memory INSERT omits pinned_personality_id.
func newCreateMemoryTestTool(t *testing.T) (*VectorStoreMemoryTool, sqlmock.Sqlmock) {
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"m","usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	t.Cleanup(server.Close)
	oai := openai.NewClient(option.WithBaseURL(server.URL), option.WithAPIKey("test-key"), option.WithMaxRetries(0))
	return NewVectorStoreMemoryTool(ds, &oai, zap.NewNop()), mock
}

// create_memory passes the chat's personality to persistence, so a User-scoped memory is pinned
// exactly when that personality has auto-pin on; a Chat-scoped memory never consults it.
func TestCreateMemoryTool_AppliesChatPersonalityAutoPin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		scope      string
		autoPin    bool
		wantPinned bool
	}{
		{name: "User scope, auto-pin on", scope: MemoryScopeUser, autoPin: true, wantPinned: true},
		{name: "User scope, auto-pin off", scope: MemoryScopeUser, autoPin: false},
		{name: "Chat scope", scope: MemoryScopeChat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tool, mock := newCreateMemoryTestTool(t)
			chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}

			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .* FROM `chats`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(chat.ID))
			if tc.scope == MemoryScopeUser {
				mock.ExpectQuery("SELECT .* FROM `personalities`").
					WithArgs(chat.PersonalityID).
					WillReturnRows(sqlmock.NewRows([]string{"id", "auto_pin_memories"}).AddRow(chat.PersonalityID, tc.autoPin))
			}
			if tc.wantPinned {
				mock.ExpectExec("INSERT INTO `memories` .*`pinned_personality_id`").WillReturnResult(sqlmock.NewResult(1, 1))
			} else {
				mock.ExpectExec("NOT pinned_personality_id").WillReturnResult(sqlmock.NewResult(1, 1))
			}
			mock.ExpectExec("INSERT INTO `embeddings`").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()

			out, err := tool.CreateMemoryTool(context.Background(), chat, []byte(`{"content":"Prefers oolong","scope":"`+tc.scope+`"}`))
			require.NoError(t, err)
			require.Contains(t, out, `"success":true`)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
