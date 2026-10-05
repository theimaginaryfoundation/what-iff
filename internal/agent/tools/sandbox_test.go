package tools

import (
	"context"
	"encoding/json"
	"fmt"
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

// Restricted chats: the tool-level half of memory sensitivity. Each test runs the same call
// against a restricted and an unrestricted chat so the gate is shown to be the only difference.

func restrictedChat(limit models.MemorySensitivity) *models.Chat {
	return &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New(), MemorySensitivityLimit: limit}
}

func mem(content string, level models.MemorySensitivity) *models.Memory {
	return &models.Memory{ID: uuid.New(), Content: content, Level: models.MemoryLevelGlobal, Scope: "User", Sensitivity: level, Confidence: 0.6, CreatedAt: time.Now()}
}

func recallJSON(t *testing.T, rt *RecallTool, chat *models.Chat, args string) recallResult {
	t.Helper()
	out, _, _, err := rt.Recall(context.Background(), chat, []byte(args))
	require.NoError(t, err)
	return decodeRecall(t, out)
}

func TestSandbox_ChatPredicates(t *testing.T) {
	chat := restrictedChat(models.MemorySensitivityPersonal)
	require.True(t, chat.MemoryRestricted())
	require.False(t, restrictedChat("").MemoryRestricted(), "an unset limit is unrestricted")
	require.False(t, restrictedChat(models.MemorySensitivitySensitive).MemoryRestricted())

	// A thread's limit is also its own classification: another conversation is readable only when
	// its own limit is at or below this chat's. Lookup failures and unknown limits refuse.
	pubPeer, perPeer, sensPeer, oddPeer, unknown := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &fakeRecallStore{chatLimits: map[uuid.UUID]models.MemorySensitivity{
		pubPeer: models.MemorySensitivityPublic, perPeer: models.MemorySensitivityPersonal,
		sensPeer: models.MemorySensitivitySensitive, oddPeer: "garbage",
	}}
	ctx := context.Background()
	require.True(t, conversationReadable(ctx, store, chat, chat.ID), "its own conversation")
	require.True(t, conversationReadable(ctx, store, chat, pubPeer))
	require.True(t, conversationReadable(ctx, store, chat, perPeer))
	require.False(t, conversationReadable(ctx, store, chat, sensPeer), "an unrestricted conversation is never readable from a restricted one")
	require.False(t, conversationReadable(ctx, store, chat, oddPeer), "an unknown stored limit fails closed")
	require.False(t, conversationReadable(ctx, store, chat, unknown), "a missing or foreign chat fails closed")
	require.False(t, conversationReadable(ctx, nil, chat, pubPeer), "no store fails closed")
	public := restrictedChat(models.MemorySensitivityPublic)
	require.True(t, conversationReadable(ctx, store, public, pubPeer))
	require.False(t, conversationReadable(ctx, store, public, perPeer))
	store.chatLimitErr = fmt.Errorf("db down")
	require.False(t, conversationReadable(ctx, store, chat, pubPeer), "a lookup error fails closed")
	calls := store.chatLimitCalls
	require.True(t, conversationReadable(ctx, store, restrictedChat(""), unknown), "an unrestricted chat reads every conversation")
	require.Equal(t, calls, store.chatLimitCalls, "and never pays for the lookup")

	// Summaries are memories with their own level: no special case beyond the level.
	otherSummary := &models.Memory{Level: models.MemoryLevelSummary, ChatID: uuid.New(), Sensitivity: models.MemorySensitivityPersonal}
	require.True(t, memoryReadableBy(chat, otherSummary))
	require.False(t, memoryReadableBy(public, otherSummary))

	require.Equal(t, models.MemorySensitivityPublic, cappedMemorySensitivity(restrictedChat(models.MemorySensitivityPublic), ""))
	require.Equal(t, models.MemorySensitivitySensitive, cappedMemorySensitivity(restrictedChat(models.MemorySensitivityPublic), "sensitive"), "an explicit sensitive is never lowered by the cap")
	require.Equal(t, models.MemorySensitivitySensitive, cappedMemorySensitivity(restrictedChat(models.MemorySensitivityPersonal), "sensitive"))
	require.Equal(t, models.MemorySensitivityPublic, cappedMemorySensitivity(restrictedChat(models.MemorySensitivityPublic), "personal"), "the personal default is capped to the limit")
	require.Equal(t, models.MemorySensitivityPersonal, cappedMemorySensitivity(restrictedChat(""), ""))
	require.Equal(t, models.MemorySensitivitySensitive, cappedMemorySensitivity(restrictedChat(""), "sensitive"))
}

func TestAgentMemorySensitivity_AllowsOnlyPersonalAndSensitive(t *testing.T) {
	for raw, want := range map[string]models.MemorySensitivity{"": "", "personal": "personal", " Sensitive ": "sensitive"} {
		got, ok := agentMemorySensitivity(raw)
		require.True(t, ok, raw)
		require.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"public", "PUBLIC", "nonsense"} {
		_, ok := agentMemorySensitivity(raw)
		require.False(t, ok, "%q must be refused", raw)
	}
}

func TestRecall_Restricted_SearchUsesLimitForMemoriesAndSummaries(t *testing.T) {
	pub, per, sens := mem("a public fact", models.MemorySensitivityPublic), mem("a personal fact", models.MemorySensitivityPersonal), mem("a sensitive fact", models.MemorySensitivitySensitive)
	store := &fakeRecallStore{
		relatedMemories: []*models.Memory{pub, per, sens},
		relatedSummaries: []*models.Memory{
			{ID: uuid.New(), Content: "summary of a public thread", Level: models.MemoryLevelSummary, ChatID: uuid.New(), Sensitivity: models.MemorySensitivityPublic},
			{ID: uuid.New(), Content: "summary of an ordinary thread", Level: models.MemoryLevelSummary, ChatID: uuid.New(), Sensitivity: models.MemorySensitivityPersonal},
		},
	}
	rt := newTestRecallTool(store)

	chat := restrictedChat(models.MemorySensitivityPersonal)
	res := recallJSON(t, rt, chat, `{"mode":"search","query":"facts"}`)
	require.Equal(t, models.MemorySensitivityPersonal, store.lastRelatedLimit, "the limit reaches the retrieval query")
	require.Equal(t, models.MemorySensitivityPersonal, store.lastSummaryMax, "and the summary query")
	require.Len(t, res.Memories, 2)
	require.NotContains(t, strings.Join(res.Memories, " "), "sensitive fact")
	require.Len(t, res.Chunks, 2)

	res = recallJSON(t, rt, restrictedChat(models.MemorySensitivityPublic), `{"mode":"search","query":"facts","source_type":"summaries"}`)
	require.Len(t, res.Chunks, 1)
	require.Contains(t, res.Chunks[0].Text, "public thread")

	// Unrestricted control: all three memories and both summaries come back.
	open := restrictedChat("")
	res = recallJSON(t, rt, open, `{"mode":"search","query":"facts"}`)
	require.Len(t, res.Memories, 3)
	require.Len(t, res.Chunks, 2)
	require.Equal(t, models.MemorySensitivitySensitive, store.lastRelatedLimit, "an unrestricted chat asks for everything")
}

func TestRecall_Restricted_FetchRelatedOriginByID(t *testing.T) {
	chatID := uuid.New()
	sens := mem("hidden fact", models.MemorySensitivitySensitive)
	per := mem("visible fact", models.MemorySensitivityPersonal)
	per.ChatID = uuid.New() // came from another conversation
	ownThread := mem("this thread's fact", models.MemorySensitivityPersonal)
	ownThread.ChatID = chatID
	peer := mem("a public thread's fact", models.MemorySensitivityPublic)
	peer.ChatID = uuid.New()
	otherSummary := &models.Memory{ID: uuid.New(), Content: "other chat summary", Level: models.MemoryLevelSummary, Sensitivity: models.MemorySensitivityPersonal, ChatID: uuid.New()}
	store := &fakeRecallStore{
		memoryByID:       map[uuid.UUID]*models.Memory{sens.ID: sens, per.ID: per, ownThread.ID: ownThread, peer.ID: peer, otherSummary.ID: otherSummary},
		relatedMemories:  []*models.Memory{per, sens, ownThread},
		summaryByChatID:  map[uuid.UUID]*models.Memory{otherSummary.ChatID: otherSummary},
		messages:         []*models.ChatMessage{{ID: uuid.New(), Message: "secret chatter", Origin: models.MessageOriginUser, SentAt: time.Now()}},
		relatedSummaries: nil,
	}
	rt := newTestRecallTool(store)
	chat := &models.Chat{ID: chatID, UserID: uuid.New(), PersonalityID: uuid.New(), MemorySensitivityLimit: models.MemorySensitivityPersonal}
	publicChat := &models.Chat{ID: chatID, UserID: chat.UserID, PersonalityID: chat.PersonalityID, MemorySensitivityLimit: models.MemorySensitivityPublic}

	// A memory above the limit resolves like a missing one, however it is addressed.
	for _, target := range []string{sens.ID.String(), "memory:" + sens.ID.String(), "memory:" + strings.ReplaceAll(sens.ID.String(), "-", "")[:10]} {
		res := recallJSON(t, rt, chat, `{"mode":"fetch","target":"`+target+`"}`)
		require.Empty(t, res.Memories, target)
		require.NotEmpty(t, res.Error, target)
		require.NotContains(t, res.Error, "hidden fact")
	}
	res := recallJSON(t, rt, chat, `{"mode":"related","target":"`+sens.ID.String()+`"}`)
	require.NotEmpty(t, res.Error, "related cannot be seeded from a hidden memory")
	res = recallJSON(t, rt, chat, `{"mode":"origin","target":"`+sens.ID.String()+`"}`)
	require.NotEmpty(t, res.Error)

	// A permitted memory is fetched normally; related excludes the hidden one in the query itself.
	res = recallJSON(t, rt, chat, `{"mode":"fetch","target":"memory:`+per.ID.String()+`"}`)
	require.Len(t, res.Memories, 1)
	res = recallJSON(t, rt, chat, `{"mode":"related","target":"`+ownThread.ID.String()+`"}`)
	require.Len(t, res.Memories, 1, "the hidden memory is filtered out of the related set")

	// origin: a permitted memory from ANOTHER conversation opens it only when that conversation's own
	// limit is at or below this chat's. per's conversation is unknown to the store, which fails
	// closed exactly like an unrestricted one.
	store.lastMsgChatID = uuid.Nil
	res = recallJSON(t, rt, chat, `{"mode":"origin","target":"`+per.ID.String()+`"}`)
	require.Len(t, res.Memories, 1)
	require.Empty(t, res.Conversations)
	require.Contains(t, res.Note, "restricted conversation")
	require.Equal(t, uuid.Nil, store.lastMsgChatID, "no message query was made for another conversation")
	// ... a peer thread at or below the limit opens ...
	store.chatLimits = map[uuid.UUID]models.MemorySensitivity{peer.ChatID: models.MemorySensitivityPublic}
	res = recallJSON(t, rt, chat, `{"mode":"origin","target":"`+peer.ID.String()+`"}`)
	require.Len(t, res.Conversations, 1)
	require.Equal(t, peer.ChatID, store.lastMsgChatID)
	// ... and so does its own conversation.
	res = recallJSON(t, rt, chat, `{"mode":"origin","target":"`+ownThread.ID.String()+`"}`)
	require.Len(t, res.Conversations, 1)

	// Another conversation's checkpoint summary is a memory with its own level: readable within the
	// limit however it is addressed, and not above it.
	for _, target := range []string{otherSummary.ChatID.String(), "summary:" + otherSummary.ChatID.String(), otherSummary.ID.String()} {
		res = recallJSON(t, rt, chat, `{"mode":"fetch","target":"`+target+`"}`)
		require.Contains(t, strings.Join(res.Memories, " ")+chunkTexts(res), "other chat summary", target)
		res = recallJSON(t, rt, publicChat, `{"mode":"fetch","target":"`+target+`"}`)
		require.Empty(t, res.Chunks, target)
		require.NotContains(t, strings.Join(res.Memories, " "), "other chat summary", target)
	}

	// Unrestricted control: the same fetches succeed.
	open := &models.Chat{ID: chatID, UserID: uuid.New(), PersonalityID: uuid.New()}
	res = recallJSON(t, rt, open, `{"mode":"fetch","target":"`+sens.ID.String()+`"}`)
	require.Len(t, res.Memories, 1)
	res = recallJSON(t, rt, open, `{"mode":"fetch","target":"summary:`+otherSummary.ChatID.String()+`"}`)
	require.Len(t, res.Chunks, 1)
}

// A restricted chat reads another conversation only when that conversation's own limit is at or
// below its own; anything else (an unrestricted thread, an unknown or failed lookup) is refused
// before a message is read.
func TestRecall_Restricted_OtherConversationsByLimit(t *testing.T) {
	pubPeer, perPeer, sensPeer := uuid.New(), uuid.New(), uuid.New()
	store := &fakeRecallStore{
		messages: []*models.ChatMessage{{ID: uuid.New(), Message: "hello", Origin: models.MessageOriginUser, SentAt: time.Now()}},
		chatLimits: map[uuid.UUID]models.MemorySensitivity{
			pubPeer: models.MemorySensitivityPublic, perPeer: models.MemorySensitivityPersonal, sensPeer: models.MemorySensitivitySensitive,
		},
	}
	rt := newTestRecallTool(store)
	chat := restrictedChat(models.MemorySensitivityPublic)

	argsFor := func(other uuid.UUID) []string {
		return []string{
			`{"mode":"conversation","target":"` + other.String() + `"}`,
			`{"mode":"bookmarks","target":"` + other.String() + `"}`,
			`{"mode":"fetch","target":"bookmark:` + other.String() + `:` + uuid.NewString() + `"}`,
		}
	}
	refused := func(chat *models.Chat, other uuid.UUID) {
		t.Helper()
		for _, args := range argsFor(other) {
			store.lastMsgChatID = uuid.Nil
			res := recallJSON(t, rt, chat, args)
			require.Contains(t, res.Error, "restricted conversation", args)
			require.Equal(t, uuid.Nil, store.lastMsgChatID, "nothing was read: %s", args)
		}
	}
	for _, other := range []uuid.UUID{perPeer, sensPeer, uuid.New()} {
		refused(chat, other)
	}
	res := recallJSON(t, rt, chat, `{"mode":"conversation","target":"`+pubPeer.String()+`"}`)
	require.Empty(t, res.Error)
	require.Len(t, res.Conversations, 1, "a public thread reads another public thread")

	// A personal-limit thread reads public and personal threads, not unrestricted ones.
	personal := restrictedChat(models.MemorySensitivityPersonal)
	for _, other := range []uuid.UUID{pubPeer, perPeer} {
		res = recallJSON(t, rt, personal, `{"mode":"conversation","target":"`+other.String()+`"}`)
		require.Empty(t, res.Error)
		require.Len(t, res.Conversations, 1)
	}
	refused(personal, sensPeer)

	// A failed lookup refuses.
	store.chatLimitErr = fmt.Errorf("db down")
	refused(personal, pubPeer)
	store.chatLimitErr = nil

	// The current conversation (by default, by sentinel, or by its own id) still works.
	for _, args := range []string{
		`{"mode":"conversation"}`,
		`{"mode":"conversation","target":"current_conversation"}`,
		`{"mode":"conversation","target":"` + chat.ID.String() + `"}`,
	} {
		res := recallJSON(t, rt, chat, args)
		require.Empty(t, res.Error, args)
		require.Len(t, res.Conversations, 1, args)
	}

	// Unrestricted control.
	res = recallJSON(t, rt, restrictedChat(""), `{"mode":"conversation","target":"`+sensPeer.String()+`"}`)
	require.Empty(t, res.Error)
	require.Len(t, res.Conversations, 1)
}

func TestRecall_Restricted_LifecycleEvents(t *testing.T) {
	member := models.MemoryMergeSourceMember{Content: "a pre-merge member that was sensitive"}
	store := &fakeRecallStore{mergeEvents: []*models.MemoryMergeEvent{{
		ID: uuid.New(), SurvivorMemoryID: uuid.New(), MergeType: models.MemoryMergeTypeFoldLive, Content: "survivor",
		SourceMembers: []models.MemoryMergeSourceMember{member}, CreatedAt: time.Now(),
	}}}
	rt := newTestRecallTool(store)

	res := recallJSON(t, rt, restrictedChat(models.MemorySensitivityPersonal), `{"mode":"lifecycle_events"}`)
	require.NotNil(t, store.lastMergeFilters.MaxSensitivity, "the limit reaches the event query")
	require.Equal(t, models.MemorySensitivityPersonal, *store.lastMergeFilters.MaxSensitivity)
	require.Len(t, res.LifecycleEvents, 1)
	require.Empty(t, res.LifecycleEvents[0].SourceMembers, "pre-merge previews are withheld from a restricted chat")

	res = recallJSON(t, rt, restrictedChat(""), `{"mode":"lifecycle_events"}`)
	require.Nil(t, store.lastMergeFilters.MaxSensitivity)
	require.Len(t, res.LifecycleEvents[0].SourceMembers, 1)
}

func TestRecall_Restricted_FilesStayInChatAndPersonalityScope(t *testing.T) {
	chatID, personalityID := uuid.New(), uuid.New()
	chat := &models.Chat{ID: chatID, UserID: uuid.New(), PersonalityID: personalityID, MemorySensitivityLimit: models.MemorySensitivityPublic}
	inChat := &models.FileAttachment{ID: uuid.New(), Name: "notes.txt", FileType: "text/plain", ChatID: &chatID}
	inPersonality := &models.FileAttachment{ID: uuid.New(), Name: "lore.txt", FileType: "text/plain", PersonalityID: &personalityID}
	elsewhereChat := uuid.New()
	elsewhere := &models.FileAttachment{ID: uuid.New(), Name: "tax-return.txt", FileType: "text/plain", ChatID: &elsewhereChat}
	store := &fakeRecallStore{
		fileByID:     map[uuid.UUID]*models.FileAttachment{inChat.ID: inChat, inPersonality.ID: inPersonality, elsewhere.ID: elsewhere},
		fileList:     []*models.FileAttachment{inChat, inPersonality, elsewhere},
		chunksForAtt: nil,
	}
	rt := newTestRecallTool(store)

	for _, fa := range []*models.FileAttachment{inChat, inPersonality} {
		res := recallJSON(t, rt, chat, `{"mode":"fetch","target":"`+fa.ID.String()+`"}`)
		require.Empty(t, res.Error, fa.Name)
	}
	res := recallJSON(t, rt, chat, `{"mode":"fetch","target":"`+elsewhere.ID.String()+`"}`)
	require.Contains(t, res.Error, "no memory, file", "a file outside the chat/personality scope reads as missing")

	// By name: the restricted lookup goes through the scoped listing, not the account-wide library.
	res = recallJSON(t, rt, chat, `{"mode":"fetch","target":"tax-return.txt"}`)
	require.Contains(t, res.Note, "No file exactly named")
	require.Positive(t, store.scopeCalls)
	require.NotContains(t, res.Note, "Nearby files", "a file outside the scope is not even suggested")
	res = recallJSON(t, rt, chat, `{"mode":"fetch","target":"notes.txt"}`)
	require.Empty(t, res.Error)
	require.NotContains(t, res.Note, "No file exactly named")

	// Unrestricted control resolves the out-of-scope file by id.
	res = recallJSON(t, rt, restrictedChat(""), `{"mode":"fetch","target":"`+elsewhere.ID.String()+`"}`)
	require.Empty(t, res.Error)
}

// --- list ---------------------------------------------------------------------

func TestList_Restricted_AccountWideKindsUnavailableAndConversationsByLimit(t *testing.T) {
	store := &fakeListStore{}
	lt := &ListTool{store: store, logger: zap.NewNop()}
	chat := restrictedChat(models.MemorySensitivityPersonal)

	run := func(chat *models.Chat, args string) listResult {
		out, err := lt.List(context.Background(), chat, []byte(args))
		require.NoError(t, err)
		var res listResult
		require.NoError(t, json.Unmarshal([]byte(out), &res))
		return res
	}

	for _, args := range []string{`{"kind":"jobs"}`, `{"kind":"skills"}`, `{"kind":"personalities"}`, `{"kind":"files"}`, `{"kind":"files","scope":"conversation"}`, `{"kind":"files","scope":"all"}`} {
		store.lastPageNum = 0
		res := run(chat, args)
		require.Contains(t, res.Error, "restricted conversation", args)
		require.Zero(t, store.lastPageNum, "the store is never queried: %s", args)
	}

	// Allowed kinds still work, including the personality's own documents.
	for _, args := range []string{`{"kind":"models"}`, `{"kind":"mcp_servers"}`, `{"kind":"files","scope":"personality"}`} {
		res := run(chat, args)
		require.Empty(t, res.Error, args)
	}

	// Conversations are listed, limited in the query to those at or below this chat's limit.
	res := run(chat, `{"kind":"conversations"}`)
	require.Empty(t, res.Error)
	require.NotNil(t, store.lastChatFilters.MaxMemorySensitivityLimit)
	require.Equal(t, models.MemorySensitivityPersonal, *store.lastChatFilters.MaxMemorySensitivityLimit)

	// Unrestricted control.
	for _, args := range []string{`{"kind":"conversations"}`, `{"kind":"jobs"}`, `{"kind":"skills"}`, `{"kind":"personalities"}`, `{"kind":"files"}`} {
		require.Empty(t, run(restrictedChat(""), args).Error, args)
	}
	run(restrictedChat(""), `{"kind":"conversations"}`)
	require.Nil(t, store.lastChatFilters.MaxMemorySensitivityLimit, "an unrestricted chat lists every conversation")
}

// --- scratchpad + create_memory -----------------------------------------------

func TestUpdateScratchpad_Restricted_Refused(t *testing.T) {
	// A nil datastore proves the tool returns before touching storage.
	st := &ScratchpadTool{logger: zap.NewNop()}
	chat := restrictedChat(models.MemorySensitivityPersonal)
	out, err := st.UpdateScratchpadTool(context.Background(), chat, []byte(`{"content":"remember this"}`))
	require.NoError(t, err)
	var res updateScratchpadToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.False(t, res.Success)
	require.Contains(t, res.Error, "restricted conversation")
}

func TestCreateMemorySpec_SensitivityIsOptionalEnum(t *testing.T) {
	prop, ok := CreateMemoryToolSpec.Properties["sensitivity"].(map[string]interface{})
	require.True(t, ok, "create_memory advertises a sensitivity parameter")
	require.Equal(t, []string{"personal", "sensitive"}, prop["enum"], "an agent can never mark a memory public")
	require.NotContains(t, CreateMemoryToolSpec.Required, "sensitivity", "it is optional; the default is personal")
	require.NotContains(t, prop["description"], "'public' (")
}

// create_memory in a restricted chat chooses scope as any chat does (only the level is capped), and
// an agent can never mark a memory public.
func TestCreateMemoryTool_Restricted_KeepsScopeCapsLevelAndRefusesPublic(t *testing.T) {
	t.Parallel()
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New(), MemorySensitivityLimit: models.MemorySensitivityPublic}

	run := func(args string, userScope bool) createMemoryToolResult {
		tool, mock := newCreateMemoryTestTool(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT .* FROM `chats`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(chat.ID))
		if userScope { // the auto-pin lookup happens for User scope only
			mock.ExpectQuery("SELECT .* FROM `personalities`").
				WithArgs(chat.PersonalityID).
				WillReturnRows(sqlmock.NewRows([]string{"id", "auto_pin_memories"}).AddRow(chat.PersonalityID, false))
		}
		mock.ExpectExec("NOT pinned_personality_id").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO `embeddings`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		out, err := tool.CreateMemoryTool(context.Background(), chat, []byte(args))
		require.NoError(t, err)
		var res createMemoryToolResult
		require.NoError(t, json.Unmarshal([]byte(out), &res))
		require.True(t, res.Success, out)
		require.NoError(t, mock.ExpectationsWereMet())
		return res
	}

	res := run(`{"content":"prefers metric units","scope":"User"}`, true)
	require.Equal(t, MemoryScopeUser, res.Scope, "a restricted chat may write an account-wide memory")
	require.Equal(t, "public", res.Sensitivity, "an unclassified memory in a public chat is capped to public")

	res = run(`{"content":"the pin is 1234","scope":"Chat","sensitivity":"sensitive"}`, false)
	require.Equal(t, MemoryScopeChat, res.Scope)
	require.Equal(t, "sensitive", res.Sensitivity, "an explicit sensitive is never lowered by the cap")

	// "public" is refused before any embedding or write happens (the tool has no datastore wired).
	bare := &VectorStoreMemoryTool{logger: zap.NewNop()}
	for _, level := range []string{"public", "bogus"} {
		out, err := bare.CreateMemoryTool(context.Background(), chat, []byte(`{"content":"x","scope":"Chat","sensitivity":"`+level+`"}`))
		require.NoError(t, err)
		require.Contains(t, out, `"success":false`)
		require.Contains(t, out, "'personal' or 'sensitive'")
	}
}

// missingMemoryStore reports every lookup the way the real datastore reports an unknown id or an
// ambiguous prefix, so the test can compare those errors with the one for a hidden memory.
type missingMemoryStore struct{ *fakeRecallStore }

func (missingMemoryStore) GetMemory(context.Context, uuid.UUID, uuid.UUID) (*models.Memory, error) {
	return nil, datastore.ErrMemoryNotFound
}

func (s missingMemoryStore) GetMemoryByIDPrefix(_ context.Context, _ uuid.UUID, prefix string) (*models.Memory, error) {
	if prefix == "aaaaaaaa" {
		return nil, fmt.Errorf("%w (%q)", datastore.ErrMemoryIDPrefixAmbiguous, prefix)
	}
	return nil, datastore.ErrMemoryNotFound
}

// A restricted chat gets the same error for an unknown id, an ambiguous prefix and a memory above
// its limit, so it cannot probe for the existence of what it may not read.
func TestResolveMemory_Restricted_HiddenLooksLikeMissing(t *testing.T) {
	hidden := mem("secret", models.MemorySensitivitySensitive)
	store := &fakeRecallStore{memoryByID: map[uuid.UUID]*models.Memory{hidden.ID: hidden}}
	restricted := restrictedChat(models.MemorySensitivityPersonal)

	want := func(target string) string { return fmt.Sprintf("memory %q not found", target) }
	cases := []struct {
		name   string
		store  recallStore
		target string
	}{
		{"hidden by id", store, hidden.ID.String()},
		{"hidden by prefix", store, hidden.ID.String()[:8]},
		{"unknown id", missingMemoryStore{store}, uuid.NewString()},
		{"unknown prefix", missingMemoryStore{store}, "deadbeef"},
		{"ambiguous prefix", missingMemoryStore{store}, "aaaaaaaa"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := newTestRecallTool(c.store).resolveMemory(context.Background(), restricted, c.target)
			require.EqualError(t, err, want(c.target))
		})
	}

	// An unrestricted chat still gets the specific errors.
	_, err := newTestRecallTool(missingMemoryStore{store}).resolveMemory(context.Background(), restrictedChat(""), "aaaaaaaa")
	require.ErrorIs(t, err, datastore.ErrMemoryIDPrefixAmbiguous)
	got, err := newTestRecallTool(store).resolveMemory(context.Background(), restrictedChat(""), hidden.ID.String())
	require.NoError(t, err)
	require.Equal(t, hidden.ID, got.ID)
}

// chunkTexts joins a recall result's chunk texts for substring assertions.
func chunkTexts(res recallResult) string {
	var parts []string
	for _, c := range res.Chunks {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, " ")
}
