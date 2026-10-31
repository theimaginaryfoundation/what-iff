package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"
)

// newSkillsAgentFixture builds an agent over an in-memory sqlite datastore with the generated
// schema (minus the pgvector tables), one user and one chat, for skill lifecycle tests.
func newSkillsAgentFixture(t *testing.T) (*Agent, *datastore.Datastore, *ent.Client, uuid.UUID, *models.Chat) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB(dialect.SQLite, db)
	m, err := schema.NewMigrate(drv)
	require.NoError(t, err)
	var tables []*schema.Table
	for _, tb := range migrate.Tables {
		if tb.Name == "embeddings" || tb.Name == "file_chunks" {
			continue
		}
		tables = append(tables, tb)
	}
	require.NoError(t, m.Create(context.Background(), tables...))
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })

	userID := uuid.New()
	require.NoError(t, client.User.Create().
		SetID(userID).
		SetUsername("skills-"+userID.String()[:8]).
		SetEmail("skills-"+userID.String()[:8]+"@example.com").
		SetPasswordHash("hash").
		Exec(context.Background()))
	chatRow, err := client.Chat.Create().SetName("c").SetOwnerID(userID).Save(context.Background())
	require.NoError(t, err)

	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)
	a := &Agent{ds: ds, logger: zap.NewNop()}
	chat := &models.Chat{ID: chatRow.ID, UserID: userID, ToolsEnabled: true}
	return a, ds, client, userID, chat
}

func boolPtr(b bool) *bool { return &b }

func createTestSkill(t *testing.T, ds *datastore.Datastore, userID uuid.UUID, r models.Ritual) *models.Ritual {
	t.Helper()
	if r.Description == "" {
		r.Description = "Use when testing " + r.Name + "."
	}
	if r.Content == "" {
		r.Content = "Instructions for " + r.Name + "."
	}
	created, err := ds.CreateRitual(context.Background(), userID, r)
	require.NoError(t, err)
	return created
}

func TestFormatSkillsSegment(t *testing.T) {
	require.Empty(t, formatSkillsSegment(nil))
	require.Empty(t, formatSkillsSegment([]*models.Ritual{{Slug: "empty", Content: "  "}}))
	got := formatSkillsSegment([]*models.Ritual{{Slug: "pdf-forms", Content: "Fill the form."}, {Name: "No Slug Yet", Content: "Body."}})
	require.True(t, strings.HasPrefix(got, skillsSegmentPreamble+"\n\n"))
	require.Contains(t, got, "<skill name=\"pdf-forms\">\nFill the form.\n</skill>")
	require.Contains(t, got, "<skill name=\"no-slug-yet\">\nBody.\n</skill>")
}

func TestRenderSkillsIndex_BudgetTiers(t *testing.T) {
	s := &turnSkills{usedEarlier: map[uuid.UUID]struct{}{}}
	hidden := &models.Ritual{ID: uuid.New(), Slug: "hidden", Description: "never listed", DisableModelInvocation: boolPtr(true)}
	active := &models.Ritual{ID: uuid.New(), Slug: "active", Description: "already active"}
	used := &models.Ritual{ID: uuid.New(), Slug: "notes", Description: "Keep notes. Use when the user wants something written down for later."}
	long := &models.Ritual{ID: uuid.New(), Slug: "pdf-forms", Description: strings.Repeat("Fill PDF forms from data. ", 20)}
	s.inScope = []*models.Ritual{long, hidden, active, used}
	s.active = []*models.Ritual{active}
	s.usedEarlier[used.ID] = struct{}{}

	full := renderSkillsIndex(s, 0)
	require.Contains(t, full, "- notes: Keep notes.")
	require.Contains(t, full, "notes: Keep notes. Use when the user wants something written down for later."+skillsIndexUsedEarlierMark)
	require.Contains(t, full, "- pdf-forms: Fill PDF forms")
	require.NotContains(t, full, "hidden")
	require.NotContains(t, full, "- active")
	require.True(t, strings.Index(full, "- notes") < strings.Index(full, "- pdf-forms"), "sorted by slug")

	// A tight budget shortens descriptions, then drops them; slugs always stay.
	short := renderSkillsIndex(s, skills.EstimateTokens(full)-20)
	require.Contains(t, short, "- pdf-forms: ")
	require.Contains(t, short, "…")
	require.Less(t, len(short), len(full))

	slugsOnly := renderSkillsIndex(s, 20)
	require.Contains(t, slugsOnly, "\n- notes"+skillsIndexUsedEarlierMark)
	require.Contains(t, slugsOnly, "\n- pdf-forms")
	require.NotContains(t, slugsOnly, "Fill PDF")
	require.Contains(t, slugsOnly, "Descriptions left out")

	require.Empty(t, renderSkillsIndex(&turnSkills{inScope: []*models.Ritual{active}, active: []*models.Ritual{active}}, 0), "nothing to list when every skill is active")
}

func TestPrepareTurnSkills_ScopeLoadedActiveAndIndex(t *testing.T) {
	a, ds, client, userID, chat := newSkillsAgentFixture(t)
	ctx := context.Background()
	pdf := createTestSkill(t, ds, userID, models.Ritual{Name: "PDF Forms"})
	notes := createTestSkill(t, ds, userID, models.Ritual{Name: "Notes"})
	hidden := createTestSkill(t, ds, userID, models.Ritual{Name: "Hidden", DisableModelInvocation: boolPtr(true)})
	modeSkill := createTestSkill(t, ds, userID, models.Ritual{Name: "Mode Skill"})
	_, err := ds.AddChatLoadedSkills(ctx, userID, chat.ID, []uuid.UUID{notes.ID})
	require.NoError(t, err)

	chatCtx := &chatContext{userID: userID, chat: chat, activeMoodRituals: []*models.Ritual{modeSkill}}
	msg := &models.ChatMessage{ChatID: chat.ID, Rituals: []*models.Ritual{{ID: pdf.ID}}}
	a.prepareTurnSkills(ctx, userID, chatCtx, msg)

	s := chatCtx.skills
	require.Len(t, s.inScope, 4)
	require.Equal(t, []uuid.UUID{notes.ID}, chatCtx.loadedSkillIDs())
	var activeSlugs []string
	for _, r := range chatCtx.activeSkills() {
		activeSlugs = append(activeSlugs, r.Slug)
	}
	require.Equal(t, []string{"mode-skill", "notes", "pdf-forms"}, activeSlugs, "loaded ∪ mode ∪ message, sorted by slug")
	require.True(t, chatCtx.skillsOffered())

	index := a.skillsIndexDeveloperContext(chatCtx)
	require.NotContains(t, index, "pdf-forms", "active skills are not listed")
	require.NotContains(t, index, "notes")
	require.NotContains(t, index, hidden.Slug, "disable_model_invocation keeps a skill out of the index")
	require.Equal(t, "", index, "every listable skill is active, so there is no index")

	// A fresh turn without the message's skill: pdf-forms is listed and marked as used earlier
	// once a message in the thread invoked it.
	_, err = client.ChatMessage.Create().SetMessage("hi").SetOrigin(chatmessage.OriginUser).SetChatID(chat.ID).AddRitualIDs(pdf.ID).Save(ctx)
	require.NoError(t, err)
	next := &chatContext{userID: userID, chat: chat}
	a.prepareTurnSkills(ctx, userID, next, &models.ChatMessage{ChatID: chat.ID})
	index = a.skillsIndexDeveloperContext(next)
	require.Contains(t, index, "- pdf-forms: Use when testing PDF Forms."+skillsIndexUsedEarlierMark)
	require.Contains(t, index, "- mode-skill: ")
	require.NotContains(t, index, "- notes", "loaded skills are active, not listed")

	// The policy brings the loaded skill into the connector scope and offers the skill tools.
	policy := a.buildTurnToolPolicy(ctx, next, userID, &models.ChatMessage{})
	require.Contains(t, policy.ritualIDs, notes.ID)
	require.False(t, policy.disabledTools[agenttools.LoadSkillToolSpec.Name])

	// A sandboxed chat sees nothing.
	sandboxed := &chatContext{userID: userID, chat: &models.Chat{ID: chat.ID, UserID: userID, ContextScope: sandboxScope(true)}}
	a.prepareTurnSkills(ctx, userID, sandboxed, msg)
	require.Empty(t, sandboxed.activeSkills())
	require.False(t, sandboxed.skillsOffered())
	require.Nil(t, sandboxed.loadedSkillIDs())
}

func TestLoadAndUnloadSkillTools(t *testing.T) {
	a, ds, _, userID, chat := newSkillsAgentFixture(t)
	ctx := context.Background()
	tools := []string{"web_search", "find_context", "not_a_tool"}
	pdf := createTestSkill(t, ds, userID, models.Ritual{Name: "PDF Forms", AllowedTools: &tools, Compatibility: strPtr("needs pypdf")})
	hidden := createTestSkill(t, ds, userID, models.Ritual{Name: "Hidden", DisableModelInvocation: boolPtr(true)})
	modeSkill := createTestSkill(t, ds, userID, models.Ritual{Name: "Mode Skill"})

	chatCtx := &chatContext{userID: userID, chat: chat, activeMoodRituals: []*models.Ritual{modeSkill}}
	a.prepareTurnSkills(ctx, userID, chatCtx, &models.ChatMessage{ChatID: chat.ID})
	offered := gatedOffered("web_search", "find_context", agenttools.LoadSkillToolSpec.Name)
	chatCtx.setOfferedTools(offered)
	a.prepareTurnLoadableTools(ctx, chatCtx, offered)

	// Unknown and non-model-invocable slugs are refused; the mode's skill is already active.
	out, err := a.loadSkillTool(ctx, chatCtx, []byte(`{"skills":["nope","`+hidden.Slug+`","mode-skill"]}`))
	require.NoError(t, err)
	var res skillLifecycleResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Empty(t, res.Loaded)
	require.Equal(t, []string{"nope", hidden.Slug}, res.Unknown)
	require.Equal(t, []string{"mode-skill"}, res.AlreadyActive)

	// Loading returns the instructions, loads the allowed built-in tool, reports the rest.
	out, err = a.loadSkillTool(ctx, chatCtx, []byte(`{"skills":["pdf-forms"]}`))
	require.NoError(t, err)
	res = skillLifecycleResult{}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Len(t, res.Loaded, 1)
	got := res.Loaded[0]
	require.Equal(t, "pdf-forms", got.Slug)
	require.Contains(t, got.Instructions, "<skill name=\"pdf-forms\">\nInstructions for PDF Forms.\n</skill>")
	require.Equal(t, "needs pypdf", got.Compatibility)
	require.Equal(t, []string{"web_search"}, got.ToolsLoaded)
	require.Equal(t, []string{"find_context"}, got.ToolsAvailable)
	require.Equal(t, []string{"not_a_tool"}, got.ToolsNotAvailable)
	require.False(t, chatCtx.builtinToolNeedsLoad("web_search"), "allowed_tools are loaded with the skill")

	require.Equal(t, []uuid.UUID{pdf.ID}, chatCtx.loadedSkillIDs())
	stored, err := ds.ListChatLoadedSkillIDs(ctx, userID, chat.ID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{pdf.ID}, stored, "persisted for later turns")
	require.True(t, chatCtx.skills.isActive(pdf.ID))

	// Loading it again is a no-op.
	out, err = a.loadSkillTool(ctx, chatCtx, []byte(`{"skills":["pdf-forms"]}`))
	require.NoError(t, err)
	res = skillLifecycleResult{}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Equal(t, []string{"pdf-forms"}, res.AlreadyActive)

	// The next turn injects it; the index no longer lists it.
	turn2 := &chatContext{userID: userID, chat: chat}
	a.prepareTurnSkills(ctx, userID, turn2, &models.ChatMessage{ChatID: chat.ID})
	require.Equal(t, []uuid.UUID{pdf.ID}, turn2.loadedSkillIDs())
	mc := &provider.ModelContext{}
	appendSkillsContext(mc, turn2.activeSkills(), a.skillsIndexDeveloperContext(turn2))
	var kinds []provider.ModelContextSegmentKind
	for _, seg := range mc.Segments {
		kinds = append(kinds, seg.Kind)
	}
	require.Equal(t, []provider.ModelContextSegmentKind{provider.SegmentKindSkills, provider.SegmentKindSkillsIndex}, kinds)
	require.Contains(t, mc.Segments[0].Content, "<skill name=\"pdf-forms\">")
	require.NotContains(t, mc.Segments[1].Content, "pdf-forms")

	// Unloading: the mode's skill is not loaded state; "all" clears what load_skill loaded.
	out, err = a.unloadSkillTool(ctx, turn2, []byte(`{"skills":["mode-skill"]}`))
	require.NoError(t, err)
	res = skillLifecycleResult{}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Equal(t, []string{"mode-skill"}, res.NotLoaded)
	out, err = a.unloadSkillTool(ctx, turn2, []byte(`{"skills":["all"]}`))
	require.NoError(t, err)
	res = skillLifecycleResult{}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Equal(t, []string{"pdf-forms"}, res.Unloaded)
	require.Empty(t, turn2.loadedSkillIDs())
	stored, err = ds.ListChatLoadedSkillIDs(ctx, userID, chat.ID)
	require.NoError(t, err)
	require.Empty(t, stored)
}

func TestUnloadSkillsUnusedInSegment(t *testing.T) {
	a, ds, client, userID, chat := newSkillsAgentFixture(t)
	ctx := context.Background()
	tools := []string{"web_search"}
	kept := createTestSkill(t, ds, userID, models.Ritual{Name: "Kept By Tool", AllowedTools: &tools})
	reloaded := createTestSkill(t, ds, userID, models.Ritual{Name: "Reloaded"})
	stale := createTestSkill(t, ds, userID, models.Ritual{Name: "Stale"})
	_, err := ds.AddChatLoadedSkills(ctx, userID, chat.ID, []uuid.UUID{kept.ID, reloaded.ID, stale.ID})
	require.NoError(t, err)

	// The segment holds one load_skill call naming "reloaded" and used web_search.
	msg, err := client.ChatMessage.Create().SetMessage("hi").SetOrigin(chatmessage.OriginAssistant).SetChatID(chat.ID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.ToolCall.Create().SetToolName(agenttools.LoadSkillToolSpec.Name).SetToolInput(`{"skills":["reloaded"]}`).SetChatMessageID(msg.ID).Exec(ctx))
	require.NoError(t, client.ToolCall.Create().SetToolName("web_search").SetToolInput(`{}`).SetChatMessageID(msg.ID).Exec(ctx))

	a.unloadToolsUnusedInSegment(ctx, userID, chat.ID, nil)

	loaded, err := ds.ListChatLoadedSkillIDs(ctx, userID, chat.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{kept.ID, reloaded.ID}, loaded, "stale was neither loaded in the segment nor had a tool it brought in called")
}

func TestSlugsFromLoadSkillInputs(t *testing.T) {
	require.Equal(t, []string{"a", "b", "c"}, slugsFromLoadSkillInputs([]string{`{"skills":["A ","b"]}`, "not json", `{"skills":["c","all"]}`}))
}
