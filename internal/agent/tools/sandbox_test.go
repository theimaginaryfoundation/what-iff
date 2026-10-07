package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Sandboxed chats: the tool-level half of the sandbox. Each test runs the same call against a
// sandboxed and an ordinary chat so the flag is shown to be the only difference.

func sandboxedChat() *models.Chat {
	return &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New(), ContextScope: models.ContextScopeSandbox}
}

func ordinaryChat() *models.Chat {
	return &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}
}

func userMem(content string) *models.Memory {
	return &models.Memory{ID: uuid.New(), Content: content, Level: models.MemoryLevelGlobal, Scope: MemoryScopeUser, Confidence: 0.6, CreatedAt: time.Now()}
}

func chatMem(content string, chatID uuid.UUID) *models.Memory {
	return &models.Memory{ID: uuid.New(), Content: content, Level: models.MemoryLevelThread, Scope: MemoryScopeChat, ChatID: chatID, Confidence: 0.6, CreatedAt: time.Now()}
}

func recallJSON(t *testing.T, rt *RecallTool, chat *models.Chat, args string) recallResult {
	t.Helper()
	out, _, _, err := rt.Recall(context.Background(), chat, []byte(args))
	require.NoError(t, err)
	return decodeRecall(t, out)
}

func TestSandbox_Predicates(t *testing.T) {
	chat, other := sandboxedChat(), uuid.New()

	// Memories: only this conversation's Chat memories and its own summary.
	require.True(t, memoryReadableBy(chat, chatMem("own", chat.ID)))
	require.False(t, memoryReadableBy(chat, chatMem("another thread's", other)))
	require.False(t, memoryReadableBy(chat, chatMem("no known chat", uuid.Nil)), "fails closed")
	require.False(t, memoryReadableBy(chat, userMem("the owner's")), "a User-scoped memory is outside the sandbox")
	require.False(t, memoryReadableBy(chat, &models.Memory{Level: models.MemoryLevelGlobal}), "scope from Level when Scope is unset")
	require.False(t, memoryReadableBy(chat, &models.Memory{}), "an unknown scope fails closed")
	require.False(t, memoryReadableBy(chat, nil))
	summaryOf := func(chatID uuid.UUID) *models.Memory {
		return &models.Memory{Level: models.MemoryLevelSummary, ChatID: chatID}
	}
	require.True(t, memoryReadableBy(chat, summaryOf(chat.ID)), "its own summary")
	require.False(t, memoryReadableBy(chat, summaryOf(other)), "another conversation's summary")

	// An ordinary chat reads everything it owns.
	open := ordinaryChat()
	require.True(t, memoryReadableBy(open, userMem("the owner's")))
	require.True(t, memoryReadableBy(open, chatMem("another thread's", other)))

	// Conversations: only itself.
	require.True(t, conversationReadable(chat, chat.ID))
	require.False(t, conversationReadable(chat, other))
	require.True(t, conversationReadable(open, other))

	// Files: only uploads to this conversation, not the personality's documents.
	own, persona, elsewhere := chat.ID, chat.PersonalityID, uuid.New()
	require.True(t, fileInChatScope(chat, &models.FileAttachment{ChatID: &own}))
	require.False(t, fileInChatScope(chat, &models.FileAttachment{PersonalityID: &persona}), "persona documents are outside the sandbox")
	require.False(t, fileInChatScope(chat, &models.FileAttachment{ChatID: &elsewhere}))
	require.False(t, fileInChatScope(chat, &models.FileAttachment{}))
	require.False(t, fileInChatScope(chat, nil))
	require.True(t, fileInChatScope(open, &models.FileAttachment{ChatID: &elsewhere}))

	// Summary search is limited to the one conversation, or open for an ordinary chat.
	require.Equal(t, chat.ID, sandboxChatID(chat))
	require.Equal(t, uuid.Nil, sandboxChatID(open))
}

func TestRecall_Sandboxed_SearchIsScopedToTheChat(t *testing.T) {
	chat := sandboxedChat()
	inside, outsideUser, outsideChat := chatMem("a fact from this thread", chat.ID), userMem("a fact about the owner"), chatMem("a fact from another thread", uuid.New())
	store := &fakeRecallStore{
		relatedMemories: []*models.Memory{inside, outsideUser, outsideChat},
		relatedSummaries: []*models.Memory{
			{ID: uuid.New(), Content: "summary of this thread", Level: models.MemoryLevelSummary, ChatID: chat.ID},
			{ID: uuid.New(), Content: "summary of another thread", Level: models.MemoryLevelSummary, ChatID: uuid.New()},
		},
	}
	rt := newTestRecallTool(store)

	res := recallJSON(t, rt, chat, `{"mode":"search","query":"facts"}`)
	require.True(t, store.lastRelatedSandboxed, "the sandbox flag reaches the retrieval query")
	require.Equal(t, chat.ID, store.lastSummaryOnlyChat, "and the summary query is limited to this chat")
	require.Len(t, res.Memories, 1)
	require.Contains(t, res.Memories[0], "this thread")
	require.Len(t, res.Chunks, 1)
	require.Contains(t, res.Chunks[0].Text, "this thread")

	// Control: an ordinary chat gets everything and searches every summary.
	res = recallJSON(t, rt, ordinaryChat(), `{"mode":"search","query":"facts"}`)
	require.False(t, store.lastRelatedSandboxed)
	require.Equal(t, uuid.Nil, store.lastSummaryOnlyChat)
	require.Len(t, res.Memories, 3)
}

func TestRecall_Sandboxed_FetchRelatedOriginByID(t *testing.T) {
	chat := sandboxedChat()
	own, hidden := chatMem("this thread's fact", chat.ID), userMem("the owner's private fact")
	elsewhere := chatMem("another thread's fact", uuid.New())
	otherSummary := &models.Memory{ID: uuid.New(), Content: "other chat summary", Level: models.MemoryLevelSummary, ChatID: uuid.New()}
	store := &fakeRecallStore{
		memoryByID:      map[uuid.UUID]*models.Memory{own.ID: own, hidden.ID: hidden, elsewhere.ID: elsewhere, otherSummary.ID: otherSummary},
		relatedMemories: []*models.Memory{own, hidden},
		summaryByChatID: map[uuid.UUID]*models.Memory{otherSummary.ChatID: otherSummary},
		messages:        []*models.ChatMessage{{ID: uuid.New(), Message: "chatter", Origin: models.MessageOriginUser, SentAt: time.Now()}},
	}
	rt := newTestRecallTool(store)

	// Anything outside the sandbox resolves like a missing id, however it is addressed.
	for _, m := range []*models.Memory{hidden, elsewhere} {
		for _, target := range []string{m.ID.String(), "memory:" + m.ID.String(), "memory:" + strings.ReplaceAll(m.ID.String(), "-", "")[:10]} {
			res := recallJSON(t, rt, chat, `{"mode":"fetch","target":"`+target+`"}`)
			require.Empty(t, res.Memories, target)
			require.NotEmpty(t, res.Error, target)
			require.NotContains(t, res.Error, "private fact")
		}
		res := recallJSON(t, rt, chat, `{"mode":"related","target":"`+m.ID.String()+`"}`)
		require.NotEmpty(t, res.Error, "related cannot be seeded from a memory outside the sandbox")
		res = recallJSON(t, rt, chat, `{"mode":"origin","target":"`+m.ID.String()+`"}`)
		require.NotEmpty(t, res.Error)
	}
	for _, target := range []string{otherSummary.ChatID.String(), "summary:" + otherSummary.ChatID.String(), otherSummary.ID.String()} {
		res := recallJSON(t, rt, chat, `{"mode":"fetch","target":"`+target+`"}`)
		require.Empty(t, res.Chunks, target)
		require.NotContains(t, strings.Join(res.Memories, " "), "other chat summary", target)
	}

	// The chat's own memory is fetched normally.
	res := recallJSON(t, rt, chat, `{"mode":"fetch","target":"memory:`+own.ID.String()+`"}`)
	require.Len(t, res.Memories, 1)

	// Unrestricted control: the same fetches succeed.
	open := ordinaryChat()
	res = recallJSON(t, rt, open, `{"mode":"fetch","target":"`+hidden.ID.String()+`"}`)
	require.Len(t, res.Memories, 1)
	res = recallJSON(t, rt, open, `{"mode":"fetch","target":"summary:`+otherSummary.ChatID.String()+`"}`)
	require.Len(t, res.Chunks, 1)
}

// A sandboxed chat reads no other conversation (messages, bookmarks, a bookmark by id, or the source
// turns of a memory) and is refused before a message is read. Its own conversation is readable.
func TestRecall_Sandboxed_OtherConversationsAreRefused(t *testing.T) {
	chat, other := sandboxedChat(), uuid.New()
	store := &fakeRecallStore{messages: []*models.ChatMessage{{ID: uuid.New(), Message: "hello", Origin: models.MessageOriginUser, SentAt: time.Now()}}}
	rt := newTestRecallTool(store)

	for _, args := range []string{
		`{"mode":"conversation","target":"` + other.String() + `"}`,
		`{"mode":"bookmarks","target":"` + other.String() + `"}`,
		`{"mode":"fetch","target":"bookmark:` + other.String() + `:` + uuid.NewString() + `"}`,
	} {
		store.lastMsgChatID = uuid.Nil
		res := recallJSON(t, rt, chat, args)
		require.Contains(t, res.Error, "sandboxed conversation", args)
		require.Equal(t, uuid.Nil, store.lastMsgChatID, "no message query was made: %s", args)
	}

	// Its own conversation reads normally.
	res := recallJSON(t, rt, chat, `{"mode":"conversation","target":"`+chat.ID.String()+`"}`)
	require.Empty(t, res.Error)
	require.Equal(t, chat.ID, store.lastMsgChatID)

	// An ordinary chat reads the other conversation.
	res = recallJSON(t, rt, ordinaryChat(), `{"mode":"conversation","target":"`+other.String()+`"}`)
	require.Empty(t, res.Error)
	require.Equal(t, other, store.lastMsgChatID)
}

func TestRecall_Sandboxed_LifecycleEventsAreLimitedToTheChat(t *testing.T) {
	chat := sandboxedChat()
	store := &fakeRecallStore{}
	rt := newTestRecallTool(store)

	recallJSON(t, rt, chat, `{"mode":"lifecycle_events"}`)
	require.NotNil(t, store.lastMergeFilters.OnlyChatID, "the store is asked for this chat's memories only")
	require.Equal(t, chat.ID, *store.lastMergeFilters.OnlyChatID)

	recallJSON(t, rt, ordinaryChat(), `{"mode":"lifecycle_events"}`)
	require.Nil(t, store.lastMergeFilters.OnlyChatID)
}

func TestRecall_Sandboxed_FilesAreOnlyTheChatsOwnUploads(t *testing.T) {
	chat := sandboxedChat()
	own, persona, elsewhere := chat.ID, chat.PersonalityID, uuid.New()
	ownFile := &models.FileAttachment{ID: uuid.New(), Name: "notes.txt", ChatID: &own}
	personaDoc := &models.FileAttachment{ID: uuid.New(), Name: "notes-lore.txt", PersonalityID: &persona}
	otherFile := &models.FileAttachment{ID: uuid.New(), Name: "notes-private.txt", ChatID: &elsewhere}
	store := &fakeRecallStore{
		fileList: []*models.FileAttachment{ownFile, personaDoc, otherFile},
		fileByID: map[uuid.UUID]*models.FileAttachment{ownFile.ID: ownFile, personaDoc.ID: personaDoc, otherFile.ID: otherFile},
	}
	rt := newTestRecallTool(store)

	// By id: only the chat's own upload.
	for _, hiddenFile := range []*models.FileAttachment{personaDoc, otherFile} {
		res := recallJSON(t, rt, chat, `{"mode":"fetch","target":"`+hiddenFile.ID.String()+`"}`)
		require.Empty(t, res.Chunks, hiddenFile.Name)
		require.NotEmpty(t, res.Error, hiddenFile.Name)
	}
	res := recallJSON(t, rt, chat, `{"mode":"fetch","target":"`+ownFile.ID.String()+`"}`)
	require.Empty(t, res.Error)

	// By name: looked up in the chat's own files, so another file is not found, and not suggested.
	store.scopeCalls = 0
	res = recallJSON(t, rt, chat, `{"mode":"fetch","target":"notes-lore.txt"}`)
	require.Positive(t, store.scopeCalls, "the lookup is scoped, not account-wide")
	require.NotContains(t, res.Note, "Nearby files", "a file outside the sandbox is not even suggested")
	require.Empty(t, res.Chunks)
}

func TestList_Sandboxed_AccountContentIsUnavailableAndFilesAreTheChatsOwn(t *testing.T) {
	chat := sandboxedChat()
	own, elsewhere := chat.ID, uuid.New()
	store := &fakeListStore{
		files: []*models.FileAttachment{
			{ID: uuid.New(), Name: "mine.txt", FileType: "text", ChatID: &own},
			{ID: uuid.New(), Name: "theirs.txt", FileType: "text", ChatID: &elsewhere},
		},
	}
	lt := &ListTool{store: store, logger: zap.NewNop()}

	for _, kind := range []string{"jobs", "skills", "personalities"} {
		out, err := lt.List(context.Background(), chat, []byte(`{"kind":"`+kind+`"}`))
		require.NoError(t, err)
		require.Contains(t, out, "sandboxed conversation", kind)
	}

	// Conversations lists exactly this one, so the model can reach its own id for find_context;
	// the store is never asked (a filter or page cannot widen it).
	chat.Name = "Discord · #general"
	out, err := lt.List(context.Background(), chat, []byte(`{"kind":"conversations","filter":"anything"}`))
	require.NoError(t, err)
	var res listResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Len(t, res.Items, 1)
	require.Equal(t, chat.ID.String(), res.Items[0].ID)
	require.Equal(t, "Discord · #general", res.Items[0].Name)
	require.Contains(t, res.Note, "only read itself")

	// Files are listed, but only the conversation's own uploads, whatever scope was asked for.
	for _, scope := range []string{"", "all", "personality", "conversation"} {
		out, err := lt.List(context.Background(), chat, []byte(`{"kind":"files","scope":"`+scope+`"}`))
		require.NoError(t, err)
		require.Contains(t, out, "mine.txt", scope)
		require.NotContains(t, out, "theirs.txt", scope)
	}

	// An ordinary chat can still list the account's files.
	out, err = lt.List(context.Background(), ordinaryChat(), []byte(`{"kind":"files"}`))
	require.NoError(t, err)
	require.Contains(t, out, "theirs.txt")
}

func TestUpdateScratchpad_Sandboxed_Refused(t *testing.T) {
	// A nil datastore proves the tool returns before touching storage.
	st := &ScratchpadTool{logger: zap.NewNop()}
	out, err := st.UpdateScratchpadTool(context.Background(), sandboxedChat(), []byte(`{"content":"remember this"}`))
	require.NoError(t, err)
	var res updateScratchpadToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.False(t, res.Success)
	require.Contains(t, res.Error, "sandboxed conversation")
}

// create_memory in a sandboxed chat always writes a Chat-scoped memory, whatever scope was asked,
// so nothing a sandbox learns reaches the owner's account.
func TestCreateMemoryTool_Sandboxed_IsAlwaysChatScoped(t *testing.T) {
	t.Parallel()
	chat := sandboxedChat()

	for _, scope := range []string{"User", "Chat", "bogus"} {
		tool, mock := newCreateMemoryTestTool(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT .* FROM `chats`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(chat.ID))
		// No auto-pin lookup: that happens for User scope only.
		mock.ExpectExec("NOT pinned_personality_id").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO `embeddings`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		out, err := tool.CreateMemoryTool(context.Background(), chat, []byte(`{"content":"prefers metric units","scope":"`+scope+`"}`))
		require.NoError(t, err)
		var res createMemoryToolResult
		require.NoError(t, json.Unmarshal([]byte(out), &res))
		require.True(t, res.Success, out)
		require.Equal(t, MemoryScopeChat, res.Scope, "asked for %q", scope)
		require.NoError(t, mock.ExpectationsWereMet(), scope)
	}
}

// missingMemoryStore reports every lookup the way the real datastore reports an unknown id or an
// ambiguous prefix, so the test can compare those errors with the one for a hidden memory.
type missingMemoryStore struct{ *fakeRecallStore }

func (missingMemoryStore) GetMemory(context.Context, uuid.UUID, uuid.UUID) (*models.Memory, error) {
	return nil, datastore.ErrMemoryNotFound
}

func (s missingMemoryStore) GetMemoryByIDPrefix(_ context.Context, _ uuid.UUID, prefix string) (*models.Memory, error) {
	if prefix == "abcd1234" {
		return nil, datastore.ErrMemoryIDPrefixAmbiguous
	}
	return nil, datastore.ErrMemoryNotFound
}

// In a sandboxed chat an unknown id, a memory outside the sandbox and an ambiguous prefix all
// return the same error, so a prefix cannot confirm that a hidden memory exists.
func TestResolveMemory_Sandboxed_HiddenLooksLikeMissing(t *testing.T) {
	chat := sandboxedChat()
	hidden := userMem("the owner's private fact")
	store := &fakeRecallStore{memoryByID: map[uuid.UUID]*models.Memory{hidden.ID: hidden}}
	rt := newTestRecallTool(store)

	_, hiddenErr := rt.resolveMemory(context.Background(), chat, hidden.ID.String())
	require.Error(t, hiddenErr)

	rt = newTestRecallTool(missingMemoryStore{store})
	_, missingErr := rt.resolveMemory(context.Background(), chat, uuid.NewString())
	require.Error(t, missingErr)
	_, ambiguousErr := rt.resolveMemory(context.Background(), chat, "memory:abcd1234")
	require.Error(t, ambiguousErr)
	for _, err := range []error{hiddenErr, missingErr, ambiguousErr} {
		require.True(t, strings.HasSuffix(err.Error(), "not found"), "every case reads as a plain miss: %v", err)
		require.NotContains(t, err.Error(), "ambiguous")
	}
}

func TestMoveFiles_Sandboxed_Refused(t *testing.T) {
	// A nil store proves the tool returns before touching the gallery.
	mt := &MoveFilesTool{logger: zap.NewNop()}
	out, err := mt.Move(context.Background(), sandboxedChat(), []byte(`{"ids":["`+uuid.NewString()+`"],"folder":"charts"}`))
	require.NoError(t, err)
	require.Contains(t, out, "sandboxed conversation")
}
