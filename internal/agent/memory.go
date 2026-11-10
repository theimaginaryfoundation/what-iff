package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var memoryExtractionSchema = provider.GenerateSchema[models.ExtractedMemoryResponse]()

// memoryExtractionSchemaMapForClaude returns the memory extraction JSON Schema as
// map[string]any for anthropic.JSONOutputFormatParam.Schema (same shape as OpenAI).
func memoryExtractionSchemaMapForClaude() map[string]any {
	return extractionSchemaCopy(memoryExtractionSchema)
}

// extractionSchemaCopy deep-copies an extraction schema, so a provider request can never mutate the
// shared value.
func extractionSchemaCopy(schema map[string]interface{}) map[string]any {
	b, err := json.Marshal(schema)
	if err != nil {
		panic("memory extraction schema: " + err.Error())
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		panic("memory extraction schema: " + err.Error())
	}
	return out
}

type ChatMessage struct {
	Role    string `json:"role"`
	Message string `json:"message"`
}

var memoryQuerySchema = provider.GenerateSchema[models.MemoryQuery]()

// memoryExtractionDeveloperMessageNoScratchpad replaces memoryExtractionDeveloperMessage in
// sandboxed chats, which have no scratchpad delta to read.
var memoryExtractionDeveloperMessageNoScratchpad = `Use the recent conversation to decide what should be written to long-term memory.
Prefer information that will remain useful later. `

// memoryExtractionDeveloperMessageFor picks the extraction developer message for a chat.
func memoryExtractionDeveloperMessageFor(chatCtx *chatContext) string {
	if chatCtx != nil && chatCtx.chat.IsSandboxed() {
		return memoryExtractionDeveloperMessageNoScratchpad
	}
	return memoryExtractionDeveloperMessage
}

var memoryExtractionDeveloperMessage = `You can see the previous scratchpad in the earlier developer message and the updated scratchpad in the latest assistant message.
Use both, along with the recent conversation, to decide what should be written to long-term memory.
Prefer information that will remain useful after it falls out of the scratchpad.
Use the conversation and scratchpad context to decide what to store or how to phrase it. `

// memoryDedupNote is the developer note for memory extraction: what to weigh,
// and the memories already stored, which must not be extracted again.
// With none stored, the note says only what to weigh.
func memoryDedupNote(chatCtx *chatContext) string {
	note := memoryExtractionDeveloperNote(chatCtx)
	if chatCtx == nil || len(chatCtx.memories) == 0 {
		return note
	}
	return fmt.Sprintf("%s\n\nDO NOT extract memories that would be duplicative of the following memories:\n\n %s",
		note, strings.Join(chatCtx.memories, "\n\n"))
}

func (a *Agent) extractMemoriesWithScratchpadDelta(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, responseID *string, inferenceModelContext *provider.ModelContext, chatCtx *chatContext, compactionEventID *uuid.UUID) {
	if responseID == nil {
		a.logger.Error("response ID is nil. cannot extract memories from conversation.")
		return
	}
	prompt := memoryWritePromptText()

	out, err := a.CallUtility(ctx, provider.UtilityRequest{
		Role:               provider.UtilityRoleMemory,
		CallPath:           telemetry.CallPathMemory,
		Instructions:       chatCtx.chat.SystemPrompt,
		PreviousResponseID: *responseID,
		History:            []provider.UtilityMessage{{Role: provider.UtilityMessageDeveloper, Text: memoryDedupNote(chatCtx)}},
		Input:              prompt,
		MaxOutputTokens:    provider.DefaultMaxContentLength,
		SafetyUserID:       userID.String(),
		Schema: &provider.UtilitySchema{
			Name:        "MemoryExtraction",
			Description: "Memory Extraction JSON",
			Schema:      memoryExtractionSchemaFor(chatCtx),
		},
	})
	if err != nil {
		a.logger.Error("failed to extract memories with scratchpad delta", zap.Error(err))
		return
	}

	extracted, err := decodeExtractedMemories(chatCtx, []byte(out))
	if err != nil {
		a.logger.Error("failed to unmarshal extracted memories", zap.Error(err))
		return
	}

	a.logger.Info("extracted memories (scratchpad delta)", zap.Any("count", len(extracted)))
	a.compactMemoriesFromCheckpoint(ctx, userID, chatID, chatCtx.chat.PersonalityID, inferenceModelContext, chatCtx, extracted, compactionEventID)
}

// extractMemoriesWithScratchpadDeltaClaude extracts long-term memories for chats whose
// checkpoint rebuilds context from the DB (every chat not on OpenAI's own Responses API;
// "Claude" is historical). It builds context from DB history instead of PreviousResponseID
// chaining, makes the scratchpad delta (old → new) explicit via context blocks, and runs on
// the memory role's model, like the chained path.
func (a *Agent) extractMemoriesWithScratchpadDeltaClaude(ctx context.Context, userID, chatID uuid.UUID, modelContext *provider.ModelContext, inferenceModelContext *provider.ModelContext, chatCtx *chatContext, compactionEventID *uuid.UUID) error {
	prompt := memoryWritePromptText()

	reply, err := a.callUtility(ctx, provider.UtilityRequest{
		Role:            provider.UtilityRoleMemory,
		CallPath:        telemetry.CallPathMemory,
		Context:         modelContext,
		History:         []provider.UtilityMessage{{Role: provider.UtilityMessageDeveloper, Text: memoryDedupNote(chatCtx)}},
		Input:           prompt,
		MaxOutputTokens: provider.DefaultMaxContentLength,
		SafetyUserID:    userID.String(),
		Schema: &provider.UtilitySchema{
			Name:        "MemoryExtraction",
			Description: "Memory Extraction JSON",
			Schema:      extractionSchemaCopy(memoryExtractionSchemaFor(chatCtx)),
		},
	})
	if err != nil {
		a.logger.Error("rebuilt-context memory extraction failed", zap.Error(err))
		return fmt.Errorf("memory extraction failed: %w", err)
	}
	extracted, err := decodeExtractedMemories(chatCtx, []byte(reply.Text))
	if err != nil {
		a.logger.Error("failed to unmarshal rebuilt-context memory extraction", zap.Error(err))
		return fmt.Errorf("failed to unmarshal memory extraction: %w", err)
	}

	a.logger.Info("extracted memories (rebuilt-context scratchpad delta)", zap.Int("count", len(extracted)))
	a.compactMemoriesFromCheckpoint(ctx, userID, chatID, chatCtx.chat.PersonalityID, inferenceModelContext, chatCtx, extracted, compactionEventID)

	return nil
}

// getMemoryQuery asks whether the user's message calls for memories, and what to search for.
// A message with no text, such as an image alone, has nothing to search with, so it doesn't.
func (a *Agent) getMemoryQuery(ctx context.Context, userID uuid.UUID, prompt string, userMessage string) (models.MemoryQuery, error) {
	if strings.TrimSpace(userMessage) == "" {
		return models.MemoryQuery{}, nil
	}

	out, err := a.CallUtility(ctx, provider.UtilityRequest{
		Role:            provider.UtilityRoleSimple,
		CallPath:        telemetry.CallPathMemory,
		Instructions:    prompt,
		Input:           userMessage,
		MaxOutputTokens: provider.DefaultMaxContentLength,
		SafetyUserID:    userID.String(),
		Schema: &provider.UtilitySchema{
			Name:        "MemoryQuery",
			Description: "Memory Query JSON",
			Schema:      memoryQuerySchema,
		},
	})
	if err != nil {
		a.logger.Error("failed to get memory query", zap.Error(err))
		return models.MemoryQuery{}, err
	}

	memoryQuery := models.MemoryQuery{}
	err = json.Unmarshal([]byte(out), &memoryQuery)
	if err != nil {
		a.logger.Error("failed to unmarshal memory query", zap.Error(err))
		return models.MemoryQuery{}, err
	}

	return memoryQuery, nil
}

// userNameContextPrefix starts the first-message line naming the user.
const userNameContextPrefix = "The user's name is "

// userNameContextLine is the first-message line naming the user.
func userNameContextLine(username string) string {
	return userNameContextPrefix + username
}

// recognizeUserNameLine returns the turn's user-name line when memories starts with it, else "".
// The line is built by the server from the user's profile, so it is recognised by being identical
// to that line, never by a text prefix on whatever the memory loader returned: a memory line always
// ends in a metadata block and can never equal it, whatever text the memory starts with. The
// prefix check below only avoids a profile lookup on turns that have no such line.
func (a *Agent) recognizeUserNameLine(ctx context.Context, userID uuid.UUID, memories []string) string {
	if len(memories) == 0 || !strings.HasPrefix(memories[0], userNameContextPrefix) || a.ds == nil {
		return ""
	}
	user, err := a.ds.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return ""
	}
	if line := userNameContextLine(user.Username); memories[0] == line {
		return line
	}
	return ""
}

// initialTurnContextLines are the lines a turn's memory context starts with: the user-name line on
// a chat's first request only (so it does not spam the context), and never in a sandboxed chat
// (strangers may be talking in it; see userNameLineAllowed).
func initialTurnContextLines(username string, hasUserMessages bool, sandboxed bool) []string {
	if !hasUserMessages && userNameLineAllowed(sandboxed) {
		return []string{userNameContextLine(username)}
	}
	return []string{}
}

// getMemories prefetches the memories relevant to userMessage for one turn. A sandboxed chat gets
// only the memories created in it (scoped in the retrieval SQL) and no user-name line.
func (a *Agent) getMemories(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, personalityID uuid.UUID, userMessage string, sandboxed bool) ([]string, []*models.Memory, error) {
	chatUser, err := a.ds.GetUserByID(ctx, userID)
	if err != nil {
		a.logger.Error("failed to get chat user", zap.Error(err))
		return nil, nil, err
	}
	hasUserMessages, err := a.ds.ChatHasMessages(ctx, userID, chatID, models.MessageOriginFilterUser)
	if err != nil {
		a.logger.Error("failed to check for user messages", zap.Error(err))
		return nil, nil, err
	}
	memories := initialTurnContextLines(chatUser.Username, hasUserMessages, sandboxed)
	liveMemories := []*models.Memory{}
	prompt := MemoryQueryPrompt
	memoryQuery, err := a.getMemoryQuery(ctx, userID, prompt, userMessage)
	if errors.Is(err, ErrUtilityRoleOff) || errors.Is(err, ErrUtilityRoleNotSet) {
		// No model to search with, by choice or not yet: the turn goes on with what it has, not
		// as a failed memory load.
		return memories, liveMemories, nil
	}
	if err != nil {
		a.logger.Error("failed to get memory query", zap.Error(err))
		return nil, nil, err
	}

	if memoryQuery.ShouldEnrich {
		memoryQueryEmbedding, err := a.memoryTool.CreateEmbedding(ctx, memoryQuery.Query)
		if err != nil {
			a.logger.Error("failed to create memory query embedding", zap.Error(err))
			return nil, nil, err
		}

		dbMemories, err := a.ds.GetRelatedMemories(ctx, userID, chatID, memoryQueryEmbedding, personalityID, sandboxed)
		if err != nil {
			a.logger.Error("failed to get related memories", zap.Error(err))
			return nil, nil, err
		}

		liveMemories = dbMemories
		if len(dbMemories) > 0 {
			memories = append(memories, memoryStalenessNote)
		}
		for _, dbMemory := range dbMemories {
			memories = append(memories, formatMemoryForContext(dbMemory))
		}
	}

	return memories, liveMemories, nil
}

func memoryWritePromptText() string {
	return fmt.Sprintf("%s\n\n%s", memoryExtractionPrompt, memoryExtractionPostamble)
}

func (a *Agent) compactMemoriesFromCheckpoint(
	ctx context.Context,
	userID uuid.UUID,
	chatID uuid.UUID,
	activePersonalityID uuid.UUID,
	inferenceModelContext *provider.ModelContext,
	chatCtx *chatContext,
	memories []models.ExtractedMemory,
	compactionEventID *uuid.UUID,
) {
	sandboxed := chatCtx != nil && chatCtx.chat.IsSandboxed()
	var liveMemories []*models.Memory
	if chatCtx != nil {
		liveMemories = chatCtx.liveMemories
	}
	if sandboxed {
		// A sandboxed chat is one a stranger may be talking in: its merge may only touch memories
		// this chat created (see sandboxedCompactionLiveMemories), never fold, rewrite or retire the
		// owner's other memories. The datastore enforces the same rule on write, and anything it
		// creates is Chat-scoped.
		liveMemories = sandboxedCompactionLiveMemories(chatID, liveMemories)
		inferenceModelContext = nil // its memory refs are the owner's wider memories; none are ours to fold
	}
	collapsed := stampExtractedProvenance(memoryutil.CollapseExtractedMemories(memories), chatCtx.externalMemoryChat())

	// DO NOT bail when nothing was freshly extracted. A checkpoint that only
	// loaded already-stored duplicates (e.g. a cluster surfaced via prefetch/find_context)
	// must still collapse — this is the roll-forward that compacts old duplication as it
	// resurfaces. buildMemoryMergeCandidates draws from the loaded set too, and
	// inferMemoryMergeGroups skips the LLM call when there is <=1 candidate, so an empty or
	// singleton segment costs nothing.

	// chatCtx.liveMemories is the complete set of memories actually loaded this turn — prefetched
	// AND anything the agent pulled in via find_context() — so it catches memories the frozen
	// MemoryRefs snapshot misses.
	candidates := buildMemoryMergeCandidates(inferenceModelContext, liveMemories, collapsed)
	if len(candidates) == 0 {
		return
	}
	// POV consistency: judge merges from the agent's own persona — the same voice that formed
	// these memories (mirrors extraction, which uses chatCtx.chat.SystemPrompt as Instructions).
	personaInstructions := ""
	if chatCtx != nil {
		personaInstructions = chatCtx.chat.SystemPrompt
	}

	// Relaxed trigger (premise: operate on the already-loaded set): we no longer require a
	// freshly-extracted member in a group, so a cluster of already-loaded duplicates may
	// collapse on its own. This stays idempotent because folded members go status=inactive and
	// are not retrieved again, so a settled cluster reduces to a singleton next checkpoint.
	groups := a.inferMemoryMergeGroups(ctx, userID, personaInstructions, candidates)
	plan := planMemoryCompaction(groups, candidates)
	a.applyMemoryCompactionPlan(ctx, userID, chatID, activePersonalityID, plan, compactionEventID, sandboxed)
}

// sandboxedCompactionLiveMemories narrows a sandboxed chat's loaded memories to the ones this chat
// created: the owner's and other chats' memories cannot be folded, rewritten or retired from here.
// The input is not mutated.
func sandboxedCompactionLiveMemories(chatID uuid.UUID, live []*models.Memory) []*models.Memory {
	own := make([]*models.Memory, 0, len(live))
	for _, mem := range live {
		if mem != nil && chatID != uuid.Nil && mem.ChatID == chatID {
			own = append(own, mem)
		}
	}
	return own
}

// mergeOptions are the datastore options for a link group: a sandboxed chat's is confined to
// memories that chat created (datastore.WithChatMemoriesOnly).
func mergeOptions(sandboxed bool) []datastore.MergeGroupOption {
	if sandboxed {
		return []datastore.MergeGroupOption{datastore.WithChatMemoriesOnly()}
	}
	return nil
}

// foldMemberOptions is mergeOptions plus the new members' combined origin. A group with no new
// member passes no origin, leaving the stored members' origins to decide the survivor's.
func foldMemberOptions(fold memoryFoldPlan, sandboxed bool) []datastore.MergeGroupOption {
	opts := mergeOptions(sandboxed)
	if fold.NewOrigin != nil {
		opts = append(opts, datastore.WithNewMemberOrigin(*fold.NewOrigin))
	}
	return opts
}

// applyMemoryCompactionPlan embeds where needed and writes fold/link plans to the datastore.
// Decision logic (no-op guards, survivor selection, link eligibility) lives in planMemoryCompaction.
// Plans treat Group as immutable — do not mutate fold.Group / link.Group before persistence.
func (a *Agent) applyMemoryCompactionPlan(
	ctx context.Context,
	userID, chatID, activePersonalityID uuid.UUID,
	plan memoryCompactionPlan,
	compactionEventID *uuid.UUID,
	sandboxed bool,
) {
	for _, fold := range plan.Folds {
		var embedding []float32
		if fold.NeedsEmbedding {
			vec, err := a.memoryTool.CreateEmbedding(ctx, fold.Group.CanonicalContent)
			switch {
			case err != nil && fold.SurvivorID == nil:
				a.logger.Error("failed to create embedding for merged memory group", zap.Error(err))
				continue
			case err != nil:
				// A survivor fold still consolidates without the embedding; it just keeps the
				// survivor's wording, since content and embedding must change together.
				a.logger.Warn("failed to embed canonical content; folding without rewriting the survivor", zap.Error(err))
			default:
				embedding = vec
			}
		}
		// PersistMemoryMergeGroup: folds into an existing survivor emit fold_live merge events (and
		// adopt the canonical phrasing when embedding is set and the survivor is not starred);
		// new-only groups (no survivor) create the row and attach it to compaction.created_memories
		// with no merge event — collapsing brand-new extractions does not change existing agent state.
		if _, err := a.ds.PersistMemoryMergeGroup(
			ctx,
			userID,
			chatID,
			fold.Group,
			fold.DuplicatesFolded,
			fold.SurvivorID,
			fold.AbsorbIDs,
			embedding,
			activePersonalityID,
			fold.SourceMembers,
			compactionEventID,
			foldMemberOptions(fold, sandboxed)...,
		); err != nil {
			a.logger.Error("failed to persist memory merge group", zap.Error(err))
		}
	}

	for _, link := range plan.Links {
		newMembers := make([]datastore.LinkGroupNewMember, 0, len(link.NewMembers))
		for _, member := range link.NewMembers {
			vec, err := a.memoryTool.CreateEmbedding(ctx, member.Content)
			if err != nil {
				a.logger.Error("failed to embed link-group member", zap.Error(err))
				continue
			}
			newMembers = append(newMembers, datastore.LinkGroupNewMember{
				Content:    member.Content,
				Confidence: member.Confidence,
				Embedding:  vec,
				Origin:     member.Origin,
			})
		}
		if len(link.ExistingIDs)+len(newMembers) < 2 {
			continue
		}
		if _, err := a.ds.PersistMemoryLinkGroup(
			ctx,
			userID,
			chatID,
			link.Scope,
			link.Group.CanonicalContent,
			link.ExistingIDs,
			newMembers,
			link.SourceMembers,
			compactionEventID,
			activePersonalityID,
			mergeOptions(sandboxed)...,
		); err != nil {
			a.logger.Error("failed to persist memory link group", zap.Error(err))
		}
	}
}
