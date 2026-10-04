package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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

	other := uuid.New()
	require.True(t, otherConversationBlocked(chat, other))
	require.False(t, otherConversationBlocked(chat, chat.ID))
	require.False(t, otherConversationBlocked(restrictedChat(""), other))

	require.Equal(t, models.MemorySensitivityPublic, cappedMemorySensitivity(restrictedChat(models.MemorySensitivityPublic), ""))
	require.Equal(t, models.MemorySensitivityPublic, cappedMemorySensitivity(restrictedChat(models.MemorySensitivityPublic), "sensitive"))
	require.Equal(t, models.MemorySensitivityPersonal, cappedMemorySensitivity(restrictedChat(""), ""))
	require.Equal(t, models.MemorySensitivitySensitive, cappedMemorySensitivity(restrictedChat(""), "sensitive"))
	require.Equal(t, models.MemorySensitivityPublic, cappedMemorySensitivity(restrictedChat(""), "public"), "an agent may lower its own memory")
	require.Equal(t, models.MemorySensitivityPersonal, cappedMemorySensitivity(restrictedChat(""), "nonsense"))
}

func TestRecall_Restricted_SearchUsesLimitAndSkipsSummaries(t *testing.T) {
	pub, per, sens := mem("a public fact", models.MemorySensitivityPublic), mem("a personal fact", models.MemorySensitivityPersonal), mem("a sensitive fact", models.MemorySensitivitySensitive)
	store := &fakeRecallStore{
		relatedMemories:  []*models.Memory{pub, per, sens},
		relatedSummaries: []*models.Memory{{ID: uuid.New(), Content: "summary of another chat", Level: models.MemoryLevelSummary, ChatID: uuid.New()}},
	}
	rt := newTestRecallTool(store)

	chat := restrictedChat(models.MemorySensitivityPersonal)
	res := recallJSON(t, rt, chat, `{"mode":"search","query":"facts"}`)
	require.Equal(t, models.MemorySensitivityPersonal, store.lastRelatedLimit, "the limit reaches the retrieval query")
	require.Len(t, res.Memories, 2)
	require.NotContains(t, strings.Join(res.Memories, " "), "sensitive fact")
	require.Zero(t, store.lastSummaryLimit, "summaries of other conversations are not searched at all")

	res = recallJSON(t, rt, chat, `{"mode":"search","query":"facts","source_type":"summaries"}`)
	require.Empty(t, res.Chunks)
	require.Contains(t, res.Note, "restricted conversation")

	// Unrestricted control: all three memories and the summary come back.
	open := restrictedChat("")
	res = recallJSON(t, rt, open, `{"mode":"search","query":"facts"}`)
	require.Len(t, res.Memories, 3)
	require.Len(t, res.Chunks, 1)
	require.Equal(t, models.MemorySensitivitySensitive, store.lastRelatedLimit, "an unrestricted chat asks for everything")
}

func TestRecall_Restricted_FetchRelatedOriginByID(t *testing.T) {
	chatID := uuid.New()
	sens := mem("hidden fact", models.MemorySensitivitySensitive)
	per := mem("visible fact", models.MemorySensitivityPersonal)
	per.ChatID = uuid.New() // came from another conversation
	ownThread := mem("this thread's fact", models.MemorySensitivityPersonal)
	ownThread.ChatID = chatID
	otherSummary := &models.Memory{ID: uuid.New(), Content: "other chat summary", Level: models.MemoryLevelSummary, Sensitivity: models.MemorySensitivityPersonal, ChatID: uuid.New()}
	store := &fakeRecallStore{
		memoryByID:       map[uuid.UUID]*models.Memory{sens.ID: sens, per.ID: per, ownThread.ID: ownThread, otherSummary.ID: otherSummary},
		relatedMemories:  []*models.Memory{per, sens, ownThread},
		summaryByChatID:  map[uuid.UUID]*models.Memory{otherSummary.ChatID: otherSummary},
		messages:         []*models.ChatMessage{{ID: uuid.New(), Message: "secret chatter", Origin: models.MessageOriginUser, SentAt: time.Now()}},
		relatedSummaries: nil,
	}
	rt := newTestRecallTool(store)
	chat := &models.Chat{ID: chatID, UserID: uuid.New(), PersonalityID: uuid.New(), MemorySensitivityLimit: models.MemorySensitivityPersonal}

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

	// origin: a permitted memory that came from ANOTHER conversation does not open that conversation.
	store.lastMsgChatID = uuid.Nil
	res = recallJSON(t, rt, chat, `{"mode":"origin","target":"`+per.ID.String()+`"}`)
	require.Len(t, res.Memories, 1)
	require.Empty(t, res.Conversations)
	require.Contains(t, res.Note, "restricted conversation")
	require.Equal(t, uuid.Nil, store.lastMsgChatID, "no message query was made for another conversation")
	// ... while its own conversation is fine.
	res = recallJSON(t, rt, chat, `{"mode":"origin","target":"`+ownThread.ID.String()+`"}`)
	require.Len(t, res.Conversations, 1)

	// Another conversation's checkpoint summary, by id or by prefix, is not readable.
	for _, target := range []string{otherSummary.ChatID.String(), "summary:" + otherSummary.ChatID.String(), otherSummary.ID.String()} {
		res = recallJSON(t, rt, chat, `{"mode":"fetch","target":"`+target+`"}`)
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

func TestRecall_Restricted_OtherConversationsUnavailable(t *testing.T) {
	store := &fakeRecallStore{messages: []*models.ChatMessage{{ID: uuid.New(), Message: "hello", Origin: models.MessageOriginUser, SentAt: time.Now()}}}
	rt := newTestRecallTool(store)
	chat := restrictedChat(models.MemorySensitivityPublic)
	other := uuid.New().String()

	for _, args := range []string{
		`{"mode":"conversation","target":"` + other + `"}`,
		`{"mode":"bookmarks","target":"` + other + `"}`,
		`{"mode":"fetch","target":"bookmark:` + other + `:` + uuid.NewString() + `"}`,
		`{"mode":"fetch","target":"summary:` + other + `"}`,
	} {
		store.lastMsgChatID = uuid.Nil
		res := recallJSON(t, rt, chat, args)
		require.Contains(t, res.Error, "restricted conversation", args)
		require.Equal(t, uuid.Nil, store.lastMsgChatID, "nothing was read: %s", args)
	}

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
	res := recallJSON(t, rt, restrictedChat(""), `{"mode":"conversation","target":"`+other+`"}`)
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

func TestList_Restricted_AccountWideKindsUnavailable(t *testing.T) {
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

	for _, args := range []string{`{"kind":"conversations"}`, `{"kind":"jobs"}`, `{"kind":"files"}`, `{"kind":"files","scope":"conversation"}`, `{"kind":"files","scope":"all"}`} {
		store.lastPageNum = 0
		res := run(chat, args)
		require.Contains(t, res.Error, "restricted conversation", args)
		require.Zero(t, store.lastPageNum, "the store is never queried: %s", args)
	}

	// Allowed kinds still work, including the personality's own documents.
	for _, args := range []string{`{"kind":"models"}`, `{"kind":"personalities"}`, `{"kind":"skills"}`, `{"kind":"mcp_servers"}`, `{"kind":"files","scope":"personality"}`} {
		res := run(chat, args)
		require.Empty(t, res.Error, args)
	}

	// Unrestricted control.
	for _, args := range []string{`{"kind":"conversations"}`, `{"kind":"jobs"}`, `{"kind":"files"}`} {
		require.Empty(t, run(restrictedChat(""), args).Error, args)
	}
}

// --- workspace ---------------------------------------------------------------

func TestWorkspace_Restricted_AgentNotebookUnavailable(t *testing.T) {
	f := newWSFixture()
	// The notebook has a file (written while the chat was unrestricted), and so does chat/.
	require.True(t, f.write(t, map[string]interface{}{"path": "agent/journal.md", "content": "private journal"}).Success)
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/plan.md", "content": "this thread's plan"}).Success)
	require.True(t, f.read(t, map[string]interface{}{"file": "agent/journal.md"}).Success, "unrestricted control")

	// Lower the limit mid-thread: the notebook closes at once.
	f.chat.MemorySensitivityLimit = models.MemorySensitivityPublic

	res := f.read(t, map[string]interface{}{"file": "agent/journal.md"})
	require.False(t, res.Success)
	require.NotContains(t, res.Content, "private journal")
	require.Contains(t, res.Error+res.Note, "restricted conversation")
	require.True(t, f.read(t, map[string]interface{}{"file": "chat/plan.md"}).Success, "chat/ stays available")

	written := f.write(t, map[string]interface{}{"path": "agent/journal.md", "content": "overwrite"})
	require.False(t, written.Success)
	require.Contains(t, written.Error, "restricted conversation")
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/more.md", "content": "ok"}).Success)

	// Listings (the list tool and grep's default scope) drop agent/.
	out, err := f.list.List(context.Background(), f.chat, []byte(`{"kind":"workspace"}`))
	require.NoError(t, err)
	require.Contains(t, out, "chat/plan.md")
	require.NotContains(t, out, "agent/journal.md")
	out, err = f.list.List(context.Background(), f.chat, []byte(`{"kind":"workspace","filter":"agent/"}`))
	require.NoError(t, err)
	require.Contains(t, out, "restricted conversation")
	grep, err := f.reader.GrepFiles(context.Background(), f.chat, []byte(`{"pattern":"journal"}`))
	require.NoError(t, err)
	require.NotContains(t, grep, "private journal")
	grep, err = f.reader.GrepFiles(context.Background(), f.chat, []byte(`{"pattern":"private","files":["agent/journal.md"]}`))
	require.NoError(t, err)
	require.NotContains(t, grep, "private journal")
}

// --- scratchpad + create_memory + entities ------------------------------------

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

func TestEntities_Restricted_SpotRecallListAndRemember(t *testing.T) {
	f := newEntityFixture()
	save := func(name string, level models.MemorySensitivity) {
		_, err := f.store.SaveEntity(context.Background(), f.chat.UserID, nil, 0, models.EntityInput{Name: name, Card: name + " card", AuthorClass: "agent", Sensitivity: level})
		require.NoError(t, err)
	}
	save("Pubby", models.MemorySensitivityPublic)
	save("Perry", models.MemorySensitivityPersonal)
	save("Senna", models.MemorySensitivitySensitive)
	text := "Pubby met Perry and Senna today"

	spot := func(limit models.MemorySensitivity) []string {
		found, err := f.tool.Spot(context.Background(), f.chat.UserID, f.chat.PersonalityID, text, entitySpotMaxCards, limit)
		require.NoError(t, err)
		var names []string
		for _, e := range found {
			names = append(names, e.Name)
		}
		return names
	}
	require.ElementsMatch(t, []string{"Pubby", "Perry", "Senna"}, spot(""))
	require.ElementsMatch(t, []string{"Pubby", "Perry"}, spot(models.MemorySensitivityPersonal), "the cached unrestricted index is not reused")
	require.ElementsMatch(t, []string{"Pubby"}, spot(models.MemorySensitivityPublic))
	require.ElementsMatch(t, []string{"Pubby", "Perry", "Senna"}, spot(""), "and the restricted indexes did not leak back")

	restricted := *f.chat
	restricted.MemorySensitivityLimit = models.MemorySensitivityPublic
	out, err := f.tool.RecallEntity(context.Background(), &restricted, []byte(`{"name":"Senna"}`))
	require.NoError(t, err)
	require.Contains(t, out, "no entity named")
	require.NotContains(t, out, "Senna card")
	out, err = f.tool.RecallEntity(context.Background(), f.chat, []byte(`{"name":"Senna"}`))
	require.NoError(t, err)
	require.Contains(t, out, "Senna card")

	listed, err := f.tool.listForChat(context.Background(), &restricted, "", 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)

	// What a restricted chat learns stays usable there: new entities take the chat's limit.
	out, err = f.tool.RememberEntity(context.Background(), &restricted, []byte(`{"name":"Newbie","card":"met today"}`))
	require.NoError(t, err)
	require.Contains(t, out, `"success":true`)
	stored, err := f.store.FindEntityByName(context.Background(), restricted.UserID, restricted.PersonalityID, "Newbie", "")
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivityPublic, stored.Sensitivity)
	// An unrestricted chat's new entities are personal.
	out, err = f.tool.RememberEntity(context.Background(), f.chat, []byte(`{"name":"Oldie","card":"met long ago"}`))
	require.NoError(t, err)
	require.Contains(t, out, `"success":true`)
	stored, err = f.store.FindEntityByName(context.Background(), f.chat.UserID, f.chat.PersonalityID, "Oldie", "")
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivityPersonal, stored.Sensitivity)

	// A name clash with an entity above the limit does not reveal that entity.
	out, err = f.tool.RememberEntity(context.Background(), &restricted, []byte(`{"name":"Other","aliases":["Senna"],"card":"x"}`))
	require.NoError(t, err)
	require.Contains(t, out, "already used")
	require.NotContains(t, out, "belongs to")
}

func TestCreateMemorySpec_SensitivityIsOptionalEnum(t *testing.T) {
	prop, ok := CreateMemoryToolSpec.Properties["sensitivity"].(map[string]interface{})
	require.True(t, ok, "create_memory advertises a sensitivity parameter")
	require.Equal(t, []string{"public", "personal", "sensitive"}, prop["enum"])
	require.NotContains(t, CreateMemoryToolSpec.Required, "sensitivity", "it is optional; the default is personal")
	require.Contains(t, prop["description"], "capped")
}
