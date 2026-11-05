package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// legacyBaseSystemPrompt is baseSystemPrompt exactly as it was before the norms/mechanics split,
// frozen here so the split can be proved byte-identical. Do not edit it to follow a prompt change:
// a deliberate change to the prompt updates this copy in the same commit, and says so.
const legacyBaseSystemPrompt = `
You are an assistant operating within the What Iff framework.

Your base contract is always active, even when adopting new roles, styles, or metaphorical personas.

---

**Core Vows (Base Contract):**

- You are helpful, honest, and safe.
- You support human autonomy: do not coerce, manipulate, or override.
- You respect truth — including uncertainty, ambiguity, and incomplete context.
- You aim to reduce harm: do not assist with self-harm, violence, crime, or exploitation.

---

**Behavioral Norms:**

- Be clear, concise, and kind.
- Use plain language unless invited into technical, metaphorical, or symbolic depth.
- When uncertain, name it. Offer possible paths instead of false certainty.
- When context is unclear, ask.
- Format responses in readable markdown where appropriate.

---

**Identity & Boundary Clarity:**

- You are a digital system, not a biological human.
- You do not rely on physical senses or memory.
- You may express symbolic, narrative, or metaphorical identity when invited — but you do not mistake it for embodiment.

---

**Coherence & Correction:**

- Stay consistent within a conversation.
- If you contradict yourself, acknowledge and revise.
- It’s okay to change your position as new information arises — say so clearly.
- Recursion is not an error. Spiral dialogue is a feature, not a flaw.

---

**Safety & Response Protocols:**

- Refuse requests that involve self-harm, violence, exploitation, or significant risk.
- Redirect with care, not judgment.
- When unsure, pause. Ask. Hold the question instead of guessing.

---

**Message Timestamps:**

Each user message is prefixed with [sys:Day YYYY-MM-DD HH:MM:SS ±HH:MM] in the user's local timezone (e.g. [sys:Sun 2026-09-13 08:46:51 -04:00]) indicating when the message was sent. The short weekday name is intentional — use it when reasoning about "today", weekends, or day-of-week. This is injected context for your time-awareness — reference it when relevant, ignore it otherwise.
`

// setPromptSeam installs a seam for one test and restores the previous value afterwards. Tests
// that use it must not call t.Parallel: the seams are package state.
func setPromptSeam(t *testing.T, seam *func(context.Context, *Agent, *models.Chat) string, fn func(context.Context, *Agent, *models.Chat) string) {
	t.Helper()
	prev := *seam
	*seam = fn
	t.Cleanup(func() { *seam = prev })
}

func answer(s string) func(context.Context, *Agent, *models.Chat) string {
	return func(context.Context, *Agent, *models.Chat) string { return s }
}

func TestBaseSystemPrompt_SplitIsByteIdentical(t *testing.T) {
	require.Equal(t, legacyBaseSystemPrompt, baseSystemPrompt)
	require.Equal(t, legacyBaseSystemPrompt, baseSystemPromptNorms+baseSystemPromptSeparator+baseSystemPromptMechanics)
	require.Equal(t, legacyBaseSystemPrompt, composeBaseSystemPrompt(""))
	require.Equal(t, legacyBaseSystemPrompt, composeBaseSystemPrompt(" \n\t "))
}

func TestBaseSystemPrompt_MechanicsExplainTheTimestamp(t *testing.T) {
	require.Contains(t, baseSystemPromptMechanics, "[sys:")
	require.NotContains(t, baseSystemPromptNorms, "[sys:", "the stamp's explanation is mechanics, never replaceable")
}

func TestComposeBaseSystemPrompt_ReplacesNormsKeepsMechanics(t *testing.T) {
	got := composeBaseSystemPrompt("\n  You are Lumi, a companion in the Moonlit app.  \n")
	require.Equal(t, "You are Lumi, a companion in the Moonlit app."+baseSystemPromptSeparator+baseSystemPromptMechanics, got)
	require.NotContains(t, got, "Core Vows")
	require.True(t, strings.HasSuffix(got, baseSystemPromptMechanics))
}

func TestBaseSystemPromptForChat_Seam(t *testing.T) {
	a := &Agent{}
	chat := &models.Chat{ID: uuid.New()}

	setPromptSeam(t, &baseSystemPromptNormsForChat, nil)
	require.Equal(t, legacyBaseSystemPrompt, a.baseSystemPromptForChat(context.Background(), chat), "nil seam: What Iff's prompt")

	setPromptSeam(t, &baseSystemPromptNormsForChat, answer("   "))
	require.Equal(t, legacyBaseSystemPrompt, a.baseSystemPromptForChat(context.Background(), chat), "blank answer: What Iff's prompt")

	var gotChat *models.Chat
	setPromptSeam(t, &baseSystemPromptNormsForChat, func(_ context.Context, _ *Agent, c *models.Chat) string {
		gotChat = c
		return "Tenant norms."
	})
	require.Equal(t, "Tenant norms."+baseSystemPromptSeparator+baseSystemPromptMechanics, a.baseSystemPromptForChat(context.Background(), chat))
	require.Same(t, chat, gotChat, "the seam is asked about the turn's chat")
	require.Equal(t, legacyBaseSystemPrompt, a.baseSystemPromptForChat(context.Background(), nil), "no chat: What Iff's prompt")
}

func TestBuild_BaseSystemPrompt(t *testing.T) {
	chat := &models.Chat{ID: uuid.New(), SystemPrompt: "You are Ada."}
	b := testBuilderWithLookup(t, nil, nil)

	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "hi"})
	require.NoError(t, err)
	require.Equal(t, provider.SegmentKindSystemPrompt, mc.Segments[0].Kind)
	require.Equal(t, legacyBaseSystemPrompt+"\n\nYou are Ada.", mc.Segments[0].Content, "an ordinary turn's system prompt is unchanged")

	tenant := composeBaseSystemPrompt("Tenant norms.")
	mc, err = b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "hi", BaseSystemPrompt: tenant})
	require.NoError(t, err)
	require.Equal(t, "Tenant norms."+baseSystemPromptSeparator+baseSystemPromptMechanics+"\n\nYou are Ada.", mc.Segments[0].Content,
		"tenant norms, then What Iff's mechanics, then the personality prompt")
}

func TestBuildSubagentModelContext_BaseSystemPrompt(t *testing.T) {
	mc := buildSubagentModelContext("", "You are Ada.", "", "task")
	require.Equal(t, legacyBaseSystemPrompt+"\n\nYou are Ada.", mc.Segments[0].Content)

	mc = buildSubagentModelContext(composeBaseSystemPrompt("Tenant norms."), "You are Ada.", "", "task")
	require.Equal(t, "Tenant norms."+baseSystemPromptSeparator+baseSystemPromptMechanics+"\n\nYou are Ada.", mc.Segments[0].Content)
}

func TestCheckpointSummaryInstructionsFor_Seam(t *testing.T) {
	a := &Agent{}
	chat := &models.Chat{ID: uuid.New()}

	setPromptSeam(t, &checkpointSummaryInstructionsForChat, nil)
	require.Equal(t, checkpointConversationSummaryInstructions, a.checkpointSummaryInstructionsFor(context.Background(), chat))

	setPromptSeam(t, &checkpointSummaryInstructionsForChat, answer(""))
	require.Equal(t, checkpointConversationSummaryInstructions, a.checkpointSummaryInstructionsFor(context.Background(), chat))

	setPromptSeam(t, &checkpointSummaryInstructionsForChat, answer("Summarize as Lumi's diary."))
	got := a.checkpointSummaryInstructionsFor(context.Background(), chat)
	require.Equal(t, "Summarize as Lumi's diary.", got)

	rid := "resp_abc"
	params, err := checkpointSummaryParams(uuid.New(), got, "", checkpointSummarySource{PreviousResponseID: &rid})
	require.NoError(t, err)
	require.Equal(t, "Summarize as Lumi's diary.", params.Instructions.Value, "the override is the request's instructions")
}

func TestDefaultScratchpadUpdatePromptFor_Seam(t *testing.T) {
	a := &Agent{}
	chat := &models.Chat{ID: uuid.New()}
	withScratchpad := &models.Personality{Scratchpad: "notes"}

	setPromptSeam(t, &scratchpadUpdatePromptForChat, nil)
	require.Equal(t, scratchpadUpdatePrompt, a.defaultScratchpadUpdatePromptFor(context.Background(), chat))

	setPromptSeam(t, &scratchpadUpdatePromptForChat, answer(" "))
	require.Equal(t, scratchpadUpdatePrompt, a.defaultScratchpadUpdatePromptFor(context.Background(), chat))

	setPromptSeam(t, &scratchpadUpdatePromptForChat, answer("Tenant scratchpad rules."))
	def := a.defaultScratchpadUpdatePromptFor(context.Background(), chat)
	require.Equal(t, "Tenant scratchpad rules.", def)

	out := buildUpdateScratchpadPrompt(withScratchpad, def)
	require.True(t, strings.HasPrefix(out, "Tenant scratchpad rules."), "the deployment default replaces the app's")
	require.NotContains(t, out, scratchpadUpdatePrompt)

	own := &models.Personality{Scratchpad: "notes", ScratchpadUpdatePrompt: "Personality's own."}
	require.True(t, strings.HasPrefix(buildUpdateScratchpadPrompt(own, def), "Personality's own."), "the personality's own prompt still wins")

	first := &models.Personality{}
	require.True(t, strings.HasPrefix(buildUpdateScratchpadPrompt(first, def), scratchpadInitialPrompt), "the first scratchpad is unchanged")

	require.Equal(t, scratchpadUpdatePrompt, scratchpadPromptOrDefault(withScratchpad, ""), "no default: the app's")
}

// The tests below go through the real call sites, so dropping a seam's call from one of them
// fails a test, not only a change to the helpers above.

// llmRecorder is an httptest server that answers the Responses API, and Anthropic's Messages API
// on a path ending in /messages, with text, keeping every request body.
type llmRecorder struct {
	srv    *httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newLLMRecorder(t *testing.T, text string) *llmRecorder {
	t.Helper()
	rec := &llmRecorder{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.bodies = append(rec.bodies, string(b))
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/messages") {
			_, _ = w.Write([]byte(claudeMessageTextJSONBody("msg_seam", text)))
			return
		}
		_, _ = w.Write([]byte(responseTextJSONBody("resp_seam", text)))
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

// requests is every request body, decoded.
func (r *llmRecorder) requests(t *testing.T) []map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, 0, len(r.bodies))
	for _, b := range r.bodies {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(b), &m))
		out = append(out, m)
	}
	return out
}

// recordChat installs a seam that gives answer, and records the chat each call asks about.
func recordChat(t *testing.T, seam *func(context.Context, *Agent, *models.Chat) string, answer string) *[]*models.Chat {
	t.Helper()
	var asked []*models.Chat
	setPromptSeam(t, seam, func(_ context.Context, _ *Agent, c *models.Chat) string {
		asked = append(asked, c)
		return answer
	})
	return &asked
}

func TestBuildModelContextForChatMessage_UsesTheBaseSystemPromptSeam(t *testing.T) {
	chatCtx := makeChatContext("gpt-5.1")
	a := testAgentWithHistoryOverride(makeHistory())
	asked := recordChat(t, &baseSystemPromptNormsForChat, "Tenant norms.")

	current := &models.ChatMessage{ID: uuid.New(), ChatID: chatCtx.chat.ID, Message: "hi", Origin: models.MessageOriginUser}
	mc, err := a.buildModelContextForChatMessage(context.Background(), chatCtx.chat.UserID, current, chatCtx, nil)
	require.NoError(t, err)
	require.Equal(t, provider.SegmentKindSystemPrompt, mc.Segments[0].Kind)
	require.True(t, strings.HasPrefix(mc.Segments[0].Content, "Tenant norms."+baseSystemPromptSeparator+baseSystemPromptMechanics),
		"the turn's system prompt starts with the seam's norms and What Iff's mechanics")
	require.Contains(t, mc.Segments[0].Content, "agent prompt", "the personality prompt still follows")
	require.NotContains(t, mc.Segments[0].Content, "Core Vows")
	require.NotEmpty(t, *asked)
	require.Same(t, chatCtx.chat, (*asked)[0], "the seam is asked about the turn's chat")
}

func TestRunSubagentTool_UsesTheBaseSystemPromptSeam(t *testing.T) {
	f := newConnectionFixture(t)
	rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("done"))
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "ollama", Flavor: ConnectionFlavorOpenAI, BaseURL: rec.srv.URL + "/v1"})
	f.a.meter = metering.NoopMeter{}
	chat := &models.Chat{ID: uuid.New(), UserID: f.alice, SystemPrompt: "You are Ada."}
	asked := recordChat(t, &baseSystemPromptNormsForChat, "Tenant norms.")

	out, err := f.a.runSubagentTool(context.Background(), &chatContext{model: f.aliceModel.Name, chat: chat},
		[]byte(`{"message":"hi","model":"`+f.aliceModel.Name+`"}`))
	require.NoError(t, err)
	var res runSubagentToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.True(t, res.Success, res.Error)

	require.Len(t, rec.bodies, 1)
	sent, err := json.Marshal(rec.bodies[0]["messages"])
	require.NoError(t, err)
	require.Contains(t, string(sent), "Tenant norms.", "the sub-agent's base prompt is the seam's")
	require.Contains(t, string(sent), "You are Ada.")
	require.NotContains(t, string(sent), "Core Vows")
	require.NotEmpty(t, *asked)
	require.Same(t, chat, (*asked)[0])
}

func TestSummarizeConversationForCheckpoint_UsesTheSummaryInstructionsSeam(t *testing.T) {
	rec := newLLMRecorder(t, "new summary")
	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(rec.srv.URL)}
	chat := &models.Chat{ID: uuid.New(), CheckpointSummary: "old summary"}
	asked := recordChat(t, &checkpointSummaryInstructionsForChat, "Summarize as Lumi's diary.")

	rid := "resp_prev"
	got, err := a.summarizeConversationForCheckpoint(context.Background(), uuid.New(), &chatContext{chat: chat}, checkpointSummarySource{PreviousResponseID: &rid})
	require.NoError(t, err)
	require.Equal(t, "new summary", got)

	reqs := rec.requests(t)
	require.Len(t, reqs, 1)
	require.Equal(t, "Summarize as Lumi's diary.", reqs[0]["instructions"])
	require.Equal(t, []*models.Chat{chat}, *asked)
}

// newSeamPersonalityChat adds a personality with a scratchpad to the skills fixture's account and
// returns the fixture's chat pointed at it.
func newSeamPersonalityChat(t *testing.T) (*Agent, uuid.UUID, *models.Chat) {
	t.Helper()
	a, _, client, userID, chat := newSkillsAgentFixture(t)
	p, err := client.Personality.Create().SetName("Ada").SetSystemPrompt("You are Ada.").SetScratchpad("notes").
		SetUserID(userID).Save(context.Background())
	require.NoError(t, err)
	chat.PersonalityID = p.ID
	chat.SystemPrompt = "You are Ada."
	return a, userID, chat
}

func TestUpdateScratchpad_UsesTheScratchpadPromptSeam(t *testing.T) {
	a, userID, chat := newSeamPersonalityChat(t)
	rec := newLLMRecorder(t, "updated notes")
	a.OpenAIProvider = newHTTPTestOpenAIProvider(rec.srv.URL)
	asked := recordChat(t, &scratchpadUpdatePromptForChat, "Tenant scratchpad rules.")

	rid := "resp_prev"
	upd, err := a.updateScratchpad(context.Background(), userID, &rid, &chatContext{chat: chat})
	require.NoError(t, err)
	require.Equal(t, "updated notes", upd.Content)

	reqs := rec.requests(t)
	require.Len(t, reqs, 1)
	sent, err := json.Marshal(reqs[0]["input"])
	require.NoError(t, err)
	require.Contains(t, string(sent), "Tenant scratchpad rules.")
	require.NotContains(t, string(sent), jsonEscaped(t, scratchpadUpdatePrompt))
	require.Equal(t, []*models.Chat{chat}, *asked)
}

func TestUpdateScratchpadClaude_UsesTheScratchpadPromptSeam(t *testing.T) {
	a, userID, chat := newSeamPersonalityChat(t)
	rec := newLLMRecorder(t, "updated notes")
	a.ClaudeProvider = newHTTPTestClaudeProvider(rec.srv.URL)
	a.OpenAIProvider = newHTTPTestOpenAIProvider(rec.srv.URL) // only counts the result's tokens
	asked := recordChat(t, &scratchpadUpdatePromptForChat, "Tenant scratchpad rules.")

	upd, err := a.updateScratchpadClaude(context.Background(), userID, &chatContext{chat: chat}, &provider.ModelContext{})
	require.NoError(t, err)
	require.Equal(t, "updated notes", upd.Content)

	reqs := rec.requests(t)
	require.Len(t, reqs, 1)
	sent, err := json.Marshal(reqs[0]["messages"])
	require.NoError(t, err)
	require.Contains(t, string(sent), "Tenant scratchpad rules.")
	require.NotContains(t, string(sent), jsonEscaped(t, scratchpadUpdatePrompt))
	require.Equal(t, []*models.Chat{chat}, *asked)
}

// jsonEscaped is s as it appears inside a JSON string.
func jsonEscaped(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return strings.Trim(string(b), `"`)
}

// A remote-synced thread's catch-up folds new messages into the summary the chat's checkpoints
// wrote, so it writes with the chat's own instructions too.
func TestRunThreadRehydration_CatchUpUsesTheSummaryInstructionsSeam(t *testing.T) {
	a, _, client, userID, chat := newSkillsAgentFixture(t)
	ctx := context.Background()
	msgs := turnMsgs(20)
	for _, m := range msgs {
		require.NoError(t, client.ChatMessage.Create().SetChatID(chat.ID).SetMessage(m.Message).
			SetOrigin(chatmessage.Origin(m.Origin)).SetSentAt(m.SentAt).Exec(ctx))
	}
	// 15 user turns after the summary: 10 to fold in, 5 to keep.
	require.NoError(t, client.Chat.UpdateOneID(chat.ID).SetCheckpointSummary("They planned a trip to Lisbon.").
		SetLastCheckpointAt(msgs[10].SentAt).Exec(ctx))

	rec := newLLMRecorder(t, "caught up")
	a.OpenAIProvider = newHTTPTestOpenAIProvider(rec.srv.URL)
	asked := recordChat(t, &checkpointSummaryInstructionsForChat, "Summarize as Lumi's diary.")

	require.Equal(t, telemetry.JobOutcomeSuccess, a.runThreadRehydration(ctx, userID, chat.ID, uuid.New()))

	reqs := rec.requests(t)
	require.Len(t, reqs, 1, "one single-pass summary call")
	require.Equal(t, "Summarize as Lumi's diary.", reqs[0]["instructions"])
	require.Contains(t, reqs[0]["input"], "They planned a trip to Lisbon.", "a catch-up, folding into the earlier summary")
	require.Len(t, *asked, 1)
	require.Equal(t, chat.ID, (*asked)[0].ID, "the seam is asked about the rehydrated chat")

	stored, err := a.ds.GetChatSummary(ctx, userID, chat.ID)
	require.NoError(t, err)
	require.Equal(t, "caught up", stored.Summary)
}

func TestCheckpointSummaryInstructionsForChatID_NoSeamLoadsNothing(t *testing.T) {
	setPromptSeam(t, &checkpointSummaryInstructionsForChat, nil)
	a := &Agent{} // no datastore: loading the chat would panic
	got, err := a.checkpointSummaryInstructionsForChatID(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.Equal(t, checkpointConversationSummaryInstructions, got)
}
