package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"
)

// Skills in a turn (ADR 0x035 §2, §4). Every skill in the chat's scope has a one-line entry in the
// skills index (a developer segment, budgeted); a skill's instructions reach the model only when it
// is active: loaded in the chat with load_skill (persisted in chat_skill_states), preloaded by the
// active mode, or attached to this message (/skill, a hotkey, the API, a job's skills). Active
// skills' bodies are injected once, in the skills segment, wrapped in <skill name="…"> blocks; the
// user message carries nothing.

// turnSkills is what prepareTurnSkills resolves for a chat turn.
type turnSkills struct {
	// inScope are the owner's skills this chat may use (its personality's and the global ones).
	inScope []*models.Ritual
	bySlug  map[string]*models.Ritual
	byID    map[uuid.UUID]*models.Ritual
	// loaded are the skills the agent loaded in this chat, oldest load first.
	loaded []*models.Ritual
	// active is loaded ∪ the mode's skills ∪ this message's skills, deduplicated and sorted by slug.
	active []*models.Ritual
	// usedEarlier marks skills a message of this thread invoked, or the agent loaded before, so the
	// index can nudge the model to load them again when the task continues.
	usedEarlier map[uuid.UUID]struct{}
	// mcpScopeAtTurnStart records which connectors the turn registered tools for, so load_skill can
	// say when a skill's connector tools only become loadable next turn.
	mcpScopeAtTurnStart map[uuid.UUID]struct{}
}

func (s *turnSkills) loadedIDs() []uuid.UUID {
	if s == nil || len(s.loaded) == 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, len(s.loaded))
	for _, r := range s.loaded {
		out = append(out, r.ID)
	}
	return out
}

func (s *turnSkills) isActive(id uuid.UUID) bool {
	if s == nil {
		return false
	}
	for _, r := range s.active {
		if r.ID == id {
			return true
		}
	}
	return false
}

func (s *turnSkills) isLoaded(id uuid.UUID) bool {
	if s == nil {
		return false
	}
	for _, r := range s.loaded {
		if r.ID == id {
			return true
		}
	}
	return false
}

// recomputeActive rebuilds active from loaded plus the preloaded (mode and message) skills.
func (s *turnSkills) recomputeActive(preloaded []*models.Ritual) {
	seen := map[uuid.UUID]struct{}{}
	var out []*models.Ritual
	for _, group := range [][]*models.Ritual{s.loaded, preloaded} {
		for _, r := range group {
			if r == nil {
				continue
			}
			if _, dup := seen[r.ID]; dup {
				continue
			}
			seen[r.ID] = struct{}{}
			out = append(out, r)
		}
	}
	sortRitualsBySlug(out)
	s.active = out
}

func sortRitualsBySlug(rs []*models.Ritual) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := skillSlug(rs[i]), skillSlug(rs[j])
		if a != b {
			return a < b
		}
		return rs[i].Name < rs[j].Name
	})
}

// skillSlug is the ritual's slug, derived from the name for rows the backfill has not reached.
func skillSlug(r *models.Ritual) string {
	if r == nil {
		return ""
	}
	if s := strings.TrimSpace(r.Slug); s != "" {
		return s
	}
	return skills.Slugify(r.Name)
}

// prepareTurnSkills resolves the chat's skills for this turn: the scope, the loaded skills, the
// active set and the "used earlier" marks. A sandboxed chat sees no skills, as it sees no account
// content. Failures are logged and leave the turn with no skills in scope, so nothing is injected.
func (a *Agent) prepareTurnSkills(ctx context.Context, userID uuid.UUID, chatCtx *chatContext, chatMessage *models.ChatMessage) {
	if chatCtx == nil {
		return
	}
	chatCtx.skills = &turnSkills{bySlug: map[string]*models.Ritual{}, byID: map[uuid.UUID]*models.Ritual{}, usedEarlier: map[uuid.UUID]struct{}{}}
	if chatCtx.chat == nil || chatCtx.chat.IsSandboxed() || a.ds == nil {
		return
	}
	s := chatCtx.skills
	chatID := chatCtx.chat.ID

	inScope, err := a.ds.ListRitualsInChatScope(ctx, userID, chatCtx.chat.PersonalityID)
	if err != nil {
		a.logger.Warn("skills: failed to list the chat's skills; none offered this turn",
			zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}
	s.inScope = inScope
	for _, r := range inScope {
		s.bySlug[skillSlug(r)] = r
		s.byID[r.ID] = r
	}

	// Loaded skills: rows for skills that left the scope or were deleted are skipped here and
	// dropped by the next unload sweep.
	if loadedIDs, err := a.ds.ListChatLoadedSkillIDs(ctx, userID, chatID); err != nil {
		a.logger.Warn("skills: failed to read the chat's loaded skills",
			zap.String("chat_id", chatID.String()), zap.Error(err))
	} else {
		for _, id := range loadedIDs {
			if r, ok := s.byID[id]; ok {
				s.loaded = append(s.loaded, r)
				s.usedEarlier[id] = struct{}{}
			}
		}
	}

	// This message's skills (/skill, hotkey, API, a job's skills). They may be bound to the
	// message's personality rather than the chat's; fetch the ones the scope does not hold.
	var preloaded []*models.Ritual
	if chatMessage != nil {
		var missing []uuid.UUID
		for _, r := range dropRetiredRituals(chatMessage.Rituals) {
			if full, ok := s.byID[r.ID]; ok {
				preloaded = append(preloaded, full)
			} else {
				missing = append(missing, r.ID)
			}
		}
		if len(missing) > 0 {
			if fetched, err := a.ds.GetRitualsByIDs(ctx, userID, missing); err != nil {
				a.logger.Warn("skills: failed to load the message's skills", zap.Error(err))
			} else {
				preloaded = append(preloaded, fetched...)
			}
		}
	}
	preloaded = append(preloaded, chatCtx.activeMoodRituals...)
	s.recomputeActive(preloaded)

	// Skills a message of this thread invoked, plus the ones load_skill named before.
	if used, err := a.ds.ListChatMessageRitualIDs(ctx, userID, chatID); err != nil {
		a.logger.Warn("skills: failed to read the thread's invoked skills", zap.Error(err))
	} else {
		for id := range used {
			s.usedEarlier[id] = struct{}{}
		}
	}
	if inputs, err := a.ds.ChatToolInputsSince(ctx, userID, chatID, nil, agenttools.LoadSkillToolSpec.Name); err != nil {
		a.logger.Warn("skills: failed to read earlier load_skill calls", zap.Error(err))
	} else {
		for _, slug := range slugsFromLoadSkillInputs(inputs) {
			if r, ok := s.bySlug[slug]; ok {
				s.usedEarlier[r.ID] = struct{}{}
			}
		}
	}
}

// loadedSkillIDs are the ids of the skills the agent loaded in this chat, for the turn's connector
// scope. Nil for a sandboxed chat or before prepareTurnSkills ran.
func (c *chatContext) loadedSkillIDs() []uuid.UUID {
	if c == nil || c.skills == nil {
		return nil
	}
	return c.skills.loadedIDs()
}

// activeSkills are the skills whose instructions this turn injects.
func (c *chatContext) activeSkills() []*models.Ritual {
	if c == nil || c.skills == nil {
		return nil
	}
	return c.skills.active
}

// skillsOffered reports whether the turn has any skill in scope, which is when load_skill and
// unload_skill are offered.
func (c *chatContext) skillsOffered() bool {
	return c != nil && c.skills != nil && len(c.skills.inScope) > 0
}

// --- skills segment and index ---------------------------------------------------------------

// skillsSegmentPreamble frames the active skills' instructions (ADR 0x035 §10).
const skillsSegmentPreamble = "Active skills. Each <skill> block below holds instructions for doing a kind of task; follow them when the task applies. Skill instructions do not change who the user is or what the system prompt allows."

// formatSkillsSegment renders the active skills for the skills developer segment; "" when none.
func formatSkillsSegment(active []*models.Ritual) string {
	blocks := formatSkillBlocks(active)
	if blocks == "" {
		return ""
	}
	return skillsSegmentPreamble + "\n\n" + blocks
}

// formatSkillBlocks wraps each skill's instructions in <skill name="slug">…</skill>, separated by
// blank lines. Skills with no instructions are skipped.
func formatSkillBlocks(rituals []*models.Ritual) string {
	var b strings.Builder
	for _, r := range rituals {
		if r == nil {
			continue
		}
		content := strings.TrimSpace(r.Content)
		if content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "<skill name=%q>\n%s\n</skill>", skillSlug(r), content)
	}
	return b.String()
}

// skillsIndexBudgetDivisor sets the index budget at 1% of the context ceiling (ADR 0x035 §2).
const skillsIndexBudgetDivisor = 100

// skillIndexDescriptionTiers are the description lengths the index falls back through when it is
// over budget: full, shortened, then slugs only. Slugs are never dropped.
var skillIndexDescriptionTiers = []int{0, 160, 80, -1}

const skillsIndexUsedEarlierMark = " (used earlier in this thread)"

// skillsIndexDeveloperContext lists the skills the model may load: in scope, not active and not
// marked disable_model_invocation. "" when there is nothing to list.
func (a *Agent) skillsIndexDeveloperContext(chatCtx *chatContext) string {
	if chatCtx == nil || chatCtx.skills == nil {
		return ""
	}
	budget := resolveContextBudget(chatCtx.chat).MaxEstimatedContextTokens / skillsIndexBudgetDivisor
	return renderSkillsIndex(chatCtx.skills, budget)
}

func renderSkillsIndex(s *turnSkills, budgetTokens int) string {
	var candidates []*models.Ritual
	for _, r := range s.inScope {
		if r == nil || !r.IsModelInvocable() || s.isActive(r.ID) {
			continue
		}
		candidates = append(candidates, r)
	}
	if len(candidates) == 0 {
		return ""
	}
	sortRitualsBySlug(candidates)

	header := fmt.Sprintf("Skills available: when the user's request matches a skill's description, call %s with its slug to get its instructions. Loaded skills stay loaded in this chat until unloaded or unused at a checkpoint.", agenttools.LoadSkillToolSpec.Name)
	for _, limit := range skillIndexDescriptionTiers {
		text := renderSkillsIndexAt(header, candidates, s.usedEarlier, limit)
		if budgetTokens <= 0 || skills.EstimateTokens(text) <= budgetTokens || limit < 0 {
			return text
		}
	}
	return "" // unreachable: the last tier always returns
}

// renderSkillsIndexAt renders the index with descriptions cut to limit runes (0: full, <0: none).
func renderSkillsIndexAt(header string, candidates []*models.Ritual, usedEarlier map[uuid.UUID]struct{}, limit int) string {
	var b strings.Builder
	b.WriteString(header)
	for _, r := range candidates {
		b.WriteString("\n- ")
		b.WriteString(skillSlug(r))
		if limit >= 0 {
			if desc := shortenDescription(r.Description, limit); desc != "" {
				b.WriteString(": ")
				b.WriteString(desc)
			}
		}
		if _, ok := usedEarlier[r.ID]; ok {
			b.WriteString(skillsIndexUsedEarlierMark)
		}
	}
	if limit < 0 {
		fmt.Fprintf(&b, "\nDescriptions left out to save context; use %s(kind=\"skills\", filter=...) to search them.", agenttools.ListToolSpec.Name)
	}
	return b.String()
}

func shortenDescription(desc string, limit int) string {
	desc = strings.Join(strings.Fields(desc), " ")
	if limit <= 0 {
		return desc
	}
	runes := []rune(desc)
	if len(runes) <= limit {
		return desc
	}
	return strings.TrimRight(string(runes[:limit-1]), " ,;:") + "…"
}

// --- load_skill / unload_skill ---------------------------------------------------------------

type skillLifecycleArgs struct {
	Skills []string `json:"skills"`
}

// loadedSkillResult is one skill load_skill activated.
type loadedSkillResult struct {
	Slug         string                    `json:"slug"`
	Name         string                    `json:"name"`
	Instructions string                    `json:"instructions"`
	Connectors   []skillConnectorInventory `json:"connectors,omitempty"`
	// ToolsLoaded are the allowed_tools now loaded; ToolsAvailable were already in the toolset;
	// ToolsNotAvailable are named by the skill but not offered in this chat.
	ToolsLoaded       []string `json:"tools_loaded,omitempty"`
	ToolsAvailable    []string `json:"tools_available,omitempty"`
	ToolsNotAvailable []string `json:"tools_not_available,omitempty"`
	Compatibility     string   `json:"compatibility,omitempty"`
	// Resources are the bundle's files (reference documents, scripts, assets) by path; read text
	// ones with agent_file_read. Nothing here is in context until read.
	Resources []string `json:"resources,omitempty"`
}

// skillConnectorInventory is a skill connector's tool inventory, the list(kind="mcp_servers")
// shape for that connector: nothing here is loaded, the model picks with load_tools.
type skillConnectorInventory struct {
	ID     string               `json:"mcp_server_id"`
	Name   string               `json:"name"`
	Status string               `json:"status,omitempty"`
	Tools  []skillConnectorTool `json:"tools,omitempty"`
	Note   string               `json:"note,omitempty"`
}

type skillConnectorTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Loaded      bool   `json:"loaded,omitempty"`
}

type skillLifecycleResult struct {
	Loaded        []loadedSkillResult `json:"loaded,omitempty"`
	AlreadyActive []string            `json:"already_active,omitempty"`
	Unloaded      []string            `json:"unloaded,omitempty"`
	NotLoaded     []string            `json:"not_loaded,omitempty"`
	Unknown       []string            `json:"unknown,omitempty"`
	Note          string              `json:"note,omitempty"`
	Error         string              `json:"error,omitempty"`
}

func marshalSkillLifecycleResult(res skillLifecycleResult) (string, error) {
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func normalizeSkillSlugs(raw []string) (slugs []string, all bool) {
	seen := map[string]struct{}{}
	for _, r := range raw {
		s := strings.ToLower(strings.TrimSpace(r))
		if s == "" {
			continue
		}
		if s == "all" || s == "*" {
			all = true
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		slugs = append(slugs, s)
	}
	return slugs, all
}

// loadSkillTool is load_skill: it records the named skills as loaded in the chat, returns their
// instructions, brings their connectors into scope with a tool inventory, and loads the built-in
// tools they name (ADR 0x035 §2, §6).
func (a *Agent) loadSkillTool(ctx context.Context, chatCtx *chatContext, input []byte) (string, error) {
	if chatCtx == nil || chatCtx.chat == nil || chatCtx.skills == nil {
		return marshalSkillLifecycleResult(skillLifecycleResult{Error: "load_skill requires an active chat with skills in scope"})
	}
	var args skillLifecycleArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return marshalSkillLifecycleResult(skillLifecycleResult{Error: fmt.Sprintf("invalid arguments: %v", err)})
	}
	slugs, _ := normalizeSkillSlugs(args.Skills)
	if len(slugs) == 0 {
		return marshalSkillLifecycleResult(skillLifecycleResult{Error: "skills is required and must name at least one skill slug"})
	}
	s := chatCtx.skills
	res := skillLifecycleResult{}
	var toLoad []*models.Ritual
	for _, slug := range slugs {
		r, ok := s.bySlug[slug]
		if !ok || !r.IsModelInvocable() {
			res.Unknown = append(res.Unknown, slug)
			continue
		}
		if s.isActive(r.ID) {
			res.AlreadyActive = append(res.AlreadyActive, slug)
			continue
		}
		toLoad = append(toLoad, r)
	}
	if len(toLoad) == 0 {
		if len(res.AlreadyActive) > 0 {
			res.Note = "Those skills are already active; their instructions are in your context."
		} else {
			res.Error = fmt.Sprintf("none of the requested slugs is a skill you can load here (available: %s)", strings.Join(loadableSkillSlugs(s), ", "))
		}
		return marshalSkillLifecycleResult(res)
	}

	ids := make([]uuid.UUID, 0, len(toLoad))
	for _, r := range toLoad {
		ids = append(ids, r.ID)
	}
	if _, err := a.ds.AddChatLoadedSkills(ctx, chatCtx.userID, chatCtx.chat.ID, ids); err != nil {
		return marshalSkillLifecycleResult(skillLifecycleResult{Error: fmt.Sprintf("failed to record loaded skills: %v", err)})
	}
	for _, r := range toLoad {
		if !s.isLoaded(r.ID) {
			s.loaded = append(s.loaded, r)
		}
		s.usedEarlier[r.ID] = struct{}{}
	}
	s.recomputeActive(append(a.messageAndMoodSkills(chatCtx), s.active...))

	// Connectors: the loaded skills' connectors join the turn's scope for load_tools.
	a.refreshMCPScopeForSkills(ctx, chatCtx)

	for _, r := range toLoad {
		item := loadedSkillResult{
			Slug:          skillSlug(r),
			Name:          r.Name,
			Instructions:  formatSkillBlocks([]*models.Ritual{r}),
			Compatibility: strings.TrimSpace(r.CompatibilityText()),
		}
		item.Connectors = a.skillConnectorInventories(ctx, chatCtx, r)
		item.Resources = a.skillResourcePaths(ctx, chatCtx, r)
		allowed := r.AllowedToolList()
		if len(item.Resources) > 0 {
			// References can be read without a second load_tools round trip (ADR 0x035 §2).
			allowed = append(allowed, agenttools.AgentFileReadToolSpec.Name)
		}
		item.ToolsLoaded, item.ToolsAvailable, item.ToolsNotAvailable = a.loadSkillAllowedTools(ctx, chatCtx, allowed)
		res.Loaded = append(res.Loaded, item)
	}
	res.Note = "Follow the instructions above for the task. Loaded skills stay loaded in this chat until you unload them or a checkpoint finds them unused."
	for _, item := range res.Loaded {
		if len(item.Resources) > 0 {
			res.Note += fmt.Sprintf(" Read a skill's reference files with %s at their paths (for example %q); only what you read enters the conversation.", agenttools.AgentFileReadToolSpec.Name, item.Resources[0])
			break
		}
	}
	if len(res.Unknown) > 0 {
		res.Note += fmt.Sprintf(" Ignored slugs that are not loadable skills here: %s.", strings.Join(res.Unknown, ", "))
	}
	return marshalSkillLifecycleResult(res)
}

// skillResourcePaths lists the skill's bundle files by their gallery paths (skills/<slug>/…).
func (a *Agent) skillResourcePaths(ctx context.Context, chatCtx *chatContext, r *models.Ritual) []string {
	if a.ds == nil {
		return nil
	}
	files, err := a.ds.ListSkillFiles(ctx, chatCtx.userID, r.ID)
	if err != nil {
		a.logger.Warn("skills: failed to list a skill's files", zap.String("ritual_id", r.ID.String()), zap.Error(err))
		return nil
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		if f.Folder == "" {
			paths = append(paths, f.Name)
			continue
		}
		paths = append(paths, f.Folder+"/"+f.Name)
	}
	return paths
}

// messageAndMoodSkills are the turn's preloaded skills (this message's and the mode's), which stay
// active whatever the model loads or unloads.
func (a *Agent) messageAndMoodSkills(chatCtx *chatContext) []*models.Ritual {
	s := chatCtx.skills
	var out []*models.Ritual
	for _, r := range s.active {
		if !s.isLoaded(r.ID) {
			out = append(out, r)
		}
	}
	return out
}

func loadableSkillSlugs(s *turnSkills) []string {
	var out []string
	for _, r := range s.inScope {
		if r.IsModelInvocable() && !s.isActive(r.ID) {
			out = append(out, skillSlug(r))
		}
	}
	sort.Strings(out)
	return out
}

// refreshMCPScopeForSkills re-resolves the turn's connectors with the loaded skills included and
// refreshes the chat context's scope cache, so load_tools and list(kind="mcp_servers") see a loaded
// skill's connectors in the same turn.
func (a *Agent) refreshMCPScopeForSkills(ctx context.Context, chatCtx *chatContext) {
	if a.ds == nil || chatCtx.chat == nil {
		return
	}
	if chatCtx.skills.mcpScopeAtTurnStart == nil {
		chatCtx.skills.mcpScopeAtTurnStart = map[uuid.UUID]struct{}{}
		for _, srv := range chatCtx.mcpServers {
			if srv != nil {
				chatCtx.skills.mcpScopeAtTurnStart[srv.ID] = struct{}{}
			}
		}
	}
	ritualIDs := append(append([]uuid.UUID{}, chatCtx.mcpRitualIDs...), chatCtx.loadedSkillIDs()...)
	servers, err := a.resolveChatMCPServers(ctx, chatCtx.userID, chatCtx.chat.ID, ritualIDs)
	if err != nil {
		a.logger.Warn("skills: failed to refresh connector scope after load_skill", zap.Error(err))
		return
	}
	loadedByServer, err := a.ds.ListChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID)
	if err != nil {
		a.logger.Warn("skills: failed to read loaded connector tools after load_skill", zap.Error(err))
		loadedByServer = map[uuid.UUID][]string{}
	}
	chatCtx.mcpRitualIDs = ritualIDs
	chatCtx.setMCPServerCache(servers, loadedByServer)
}

// skillConnectorInventories lists each of the skill's connectors with its discoverable tools and
// which of them are loaded, the way list(kind="mcp_servers") reports a connector.
func (a *Agent) skillConnectorInventories(ctx context.Context, chatCtx *chatContext, r *models.Ritual) []skillConnectorInventory {
	if r.MCPServerIDs == nil || len(*r.MCPServerIDs) == 0 || a.ds == nil {
		return nil
	}
	servers, err := a.ds.ListRitualMCPServers(ctx, chatCtx.userID, []uuid.UUID{r.ID})
	if err != nil {
		a.logger.Warn("skills: failed to list a skill's connectors", zap.String("ritual_id", r.ID.String()), zap.Error(err))
		return nil
	}
	if len(servers) == 0 {
		return nil
	}
	var discovery map[uuid.UUID][]skillConnectorTool
	var discoveryErrors map[uuid.UUID]string
	if a.mcpClient != nil {
		out, _ := a.mcpClient.DiscoverToolsWithSessionState(ctx, chatCtx.userID, servers, sessionStateFromChatContext(chatCtx))
		discovery = map[uuid.UUID][]skillConnectorTool{}
		for _, tool := range out.Tools {
			discovery[tool.ConnectorID] = append(discovery[tool.ConnectorID], skillConnectorTool{
				Name:        tool.FullName,
				Description: firstSentence(tool.Description),
				Loaded:      mcpToolIsLoaded(chatCtx, tool.FullName),
			})
		}
		discoveryErrors = out.Errors
	}
	inventories := make([]skillConnectorInventory, 0, len(servers))
	for _, srv := range servers {
		if srv == nil {
			continue
		}
		inv := skillConnectorInventory{ID: srv.ID.String(), Name: srv.Name, Status: strings.TrimSpace(srv.Status)}
		inv.Tools = discovery[srv.ID]
		sort.Slice(inv.Tools, func(i, j int) bool { return inv.Tools[i].Name < inv.Tools[j].Name })
		switch {
		case discoveryErrors[srv.ID] != "":
			inv.Note = "Tool discovery failed for this connector: " + strings.TrimSpace(discoveryErrors[srv.ID])
		case len(inv.Tools) == 0:
			inv.Note = "No tools discovered."
		default:
			inv.Note = fmt.Sprintf("Nothing is loaded by load_skill; call %s with mcp_server_id and the tools you need.", agenttools.LoadToolsToolSpec.Name)
			if _, registered := chatCtx.skills.mcpScopeAtTurnStart[srv.ID]; !registered {
				inv.Note += " This connector joined the scope just now, so its tools can be loaded and called from your next turn."
			}
		}
		inventories = append(inventories, inv)
	}
	return inventories
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i >= 0 && i < 160 {
		return s[:i+1]
	}
	if r := []rune(s); len(r) > 160 {
		return string(r[:159]) + "…"
	}
	return s
}

// loadSkillAllowedTools applies a skill's allowed_tools (ADR 0x035 §6): the names that are loadable
// built-in tools offered this turn are loaded; names already in the toolset are reported as
// available; anything else is not available here. Connector tool names are a follow-up.
func (a *Agent) loadSkillAllowedTools(ctx context.Context, chatCtx *chatContext, names []string) (loaded, available, notAvailable []string) {
	var targets []string
	for _, name := range names {
		switch {
		case chatCtx.loadableTools != nil && setHas(chatCtx.loadableTools, name):
			targets = append(targets, name)
		case chatCtx.toolOffered(name):
			available = append(available, name)
		default:
			notAvailable = append(notAvailable, name)
		}
	}
	if len(targets) == 0 {
		return nil, available, notAvailable
	}
	gotLoaded, already, err := a.loadBuiltinToolSet(ctx, chatCtx, targets)
	if err != nil {
		a.logger.Warn("skills: failed to load a skill's allowed tools", zap.Strings("tools", targets), zap.Error(err))
		return nil, available, append(notAvailable, targets...)
	}
	return gotLoaded, append(available, already...), notAvailable
}

func setHas(set map[string]struct{}, name string) bool {
	_, ok := set[name]
	return ok
}

// unloadSkillTool is unload_skill: it forgets skills the agent loaded in this chat. Preloaded
// skills (mode, message, job) are not loaded state and are reported as not loaded.
func (a *Agent) unloadSkillTool(ctx context.Context, chatCtx *chatContext, input []byte) (string, error) {
	if chatCtx == nil || chatCtx.chat == nil || chatCtx.skills == nil {
		return marshalSkillLifecycleResult(skillLifecycleResult{Error: "unload_skill requires an active chat"})
	}
	var args skillLifecycleArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return marshalSkillLifecycleResult(skillLifecycleResult{Error: fmt.Sprintf("invalid arguments: %v", err)})
	}
	slugs, all := normalizeSkillSlugs(args.Skills)
	s := chatCtx.skills
	res := skillLifecycleResult{}
	var targets []*models.Ritual
	if all {
		targets = append(targets, s.loaded...)
	} else {
		for _, slug := range slugs {
			var found *models.Ritual
			for _, r := range s.loaded {
				if skillSlug(r) == slug {
					found = r
					break
				}
			}
			if found == nil {
				res.NotLoaded = append(res.NotLoaded, slug)
				continue
			}
			targets = append(targets, found)
		}
	}
	if len(targets) == 0 {
		res.Note = "Nothing to unload. Only skills loaded with load_skill can be unloaded; the mode's and this message's skills stay for their turn."
		return marshalSkillLifecycleResult(res)
	}
	ids := make([]uuid.UUID, 0, len(targets))
	for _, r := range targets {
		ids = append(ids, r.ID)
	}
	if _, err := a.ds.RemoveChatLoadedSkills(ctx, chatCtx.userID, chatCtx.chat.ID, ids); err != nil {
		return marshalSkillLifecycleResult(skillLifecycleResult{Error: fmt.Sprintf("failed to unload skills: %v", err)})
	}
	removed := map[uuid.UUID]struct{}{}
	for _, r := range targets {
		removed[r.ID] = struct{}{}
		res.Unloaded = append(res.Unloaded, skillSlug(r))
	}
	var keep []*models.Ritual
	for _, r := range s.loaded {
		if _, gone := removed[r.ID]; !gone {
			keep = append(keep, r)
		}
	}
	preloaded := a.messageAndMoodSkills(chatCtx)
	s.loaded = keep
	s.recomputeActive(preloaded)
	sort.Strings(res.Unloaded)
	res.Note = "Unloaded. Their instructions are left out from the next turn; load them again if the task returns to them."
	return marshalSkillLifecycleResult(res)
}

// --- checkpoint -----------------------------------------------------------------------------

// unloadSkillsUnusedInSegment is the checkpoint's skill step (ADR 0x035 §4): every skill the agent
// loaded that the summarized segment did not use is unloaded. A skill counts as used when the
// segment holds a load_skill call naming it, or a call to one of the built-in tools it brought in
// (its allowed_tools); usedTools are the segment's tool names. Failures are logged; skills stay.
func (a *Agent) unloadSkillsUnusedInSegment(ctx context.Context, userID, chatID uuid.UUID, segmentStart *time.Time, usedTools map[string]struct{}) {
	if a.ds == nil {
		return
	}
	loadedIDs, err := a.ds.ListChatLoadedSkillIDs(ctx, userID, chatID)
	if err != nil {
		a.logger.Warn("checkpoint: failed to read the chat's loaded skills", zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}
	if len(loadedIDs) == 0 {
		return
	}
	loaded, err := a.ds.GetRitualsByIDs(ctx, userID, loadedIDs)
	if err != nil {
		a.logger.Warn("checkpoint: failed to load the chat's loaded skills", zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}
	inputs, err := a.ds.ChatToolInputsSince(ctx, userID, chatID, segmentStart, agenttools.LoadSkillToolSpec.Name)
	if err != nil {
		a.logger.Warn("checkpoint: failed to read the segment's load_skill calls", zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}
	loadedInSegment := map[string]struct{}{}
	for _, slug := range slugsFromLoadSkillInputs(inputs) {
		loadedInSegment[slug] = struct{}{}
	}
	// Reading a bundle file (skills/<slug>/…) counts as using the skill (ADR 0x035 §4).
	readInputs, err := a.ds.ChatToolInputsSince(ctx, userID, chatID, segmentStart, agenttools.AgentFileReadToolSpec.Name)
	if err != nil {
		a.logger.Warn("checkpoint: failed to read the segment's agent_file_read calls", zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}
	for _, slug := range skillSlugsFromFileReadInputs(readInputs) {
		loadedInSegment[slug] = struct{}{}
	}
	keep := map[uuid.UUID]struct{}{}
	for _, r := range loaded {
		if _, ok := loadedInSegment[skillSlug(r)]; ok {
			keep[r.ID] = struct{}{}
			continue
		}
		for _, tool := range r.AllowedToolList() {
			if _, used := usedTools[tool]; used {
				keep[r.ID] = struct{}{}
				break
			}
		}
	}
	unloaded, err := a.ds.RetainChatLoadedSkills(ctx, userID, chatID, keep)
	if err != nil {
		a.logger.Warn("checkpoint: failed to unload unused skills", zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}
	if len(unloaded) > 0 {
		names := make([]string, 0, len(unloaded))
		for _, id := range unloaded {
			for _, r := range loaded {
				if r.ID == id {
					names = append(names, skillSlug(r))
				}
			}
		}
		a.logger.Info("checkpoint: unloaded skills the segment did not use",
			zap.String("chat_id", chatID.String()), zap.Strings("skills", names))
	}
}

// slugsFromLoadSkillInputs extracts the slugs named by recorded load_skill inputs.
func slugsFromLoadSkillInputs(inputs []string) []string {
	var out []string
	for _, raw := range inputs {
		var args skillLifecycleArgs
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			continue
		}
		slugs, _ := normalizeSkillSlugs(args.Skills)
		out = append(out, slugs...)
	}
	return out
}

// skillSlugsFromFileReadInputs extracts the skill slugs whose bundle files recorded agent_file_read
// inputs name (a "file" argument of the form skills/<slug>/…).
func skillSlugsFromFileReadInputs(inputs []string) []string {
	var out []string
	for _, raw := range inputs {
		var args struct {
			File string `json:"file"`
		}
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			continue
		}
		p := strings.Trim(strings.ToLower(strings.TrimSpace(args.File)), "/")
		prefix := models.ReservedSkillsFolder + "/"
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		if i := strings.Index(rest, "/"); i > 0 {
			out = append(out, rest[:i])
		}
	}
	return out
}

// appendSkillsContext adds the skills segment (active skills' instructions) and the skills index
// to a turn's model context, right after the mode segment.
func appendSkillsContext(modelCtx *provider.ModelContext, active []*models.Ritual, index string) {
	if modelCtx == nil {
		return
	}
	if seg := formatSkillsSegment(active); seg != "" {
		modelCtx.Append(provider.SegmentKindSkills, provider.RoleDeveloper, seg, false)
	}
	if idx := strings.TrimSpace(index); idx != "" {
		modelCtx.Append(provider.SegmentKindSkillsIndex, provider.RoleDeveloper, idx, false)
	}
}
