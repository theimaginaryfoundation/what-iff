package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
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
	params, err := buildCheckpointSummaryParams(uuid.New(), got, "", checkpointSummarySource{PreviousResponseID: &rid})
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
