package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"go.uber.org/zap"
)

var memoryExtractionSchema = provider.GenerateSchema[models.ExtractedMemoryResponse]()

// memoryExtractionSchemaMapForClaude returns the memory extraction JSON Schema as
// map[string]any for anthropic.JSONOutputFormatParam.Schema (same shape as OpenAI).
func memoryExtractionSchemaMapForClaude() map[string]any {
	b, err := json.Marshal(memoryExtractionSchema)
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

// memoryExtractionDeveloperMessageNoScratchpad replaces memoryExtractionDeveloperMessage in restricted
// chats, which have no scratchpad delta to read.
var memoryExtractionDeveloperMessageNoScratchpad = `Use the recent conversation to decide what should be written to long-term memory.
Prefer information that will remain useful later. `

// memoryExtractionDeveloperMessageFor picks the extraction developer message for a chat.
func memoryExtractionDeveloperMessageFor(chatCtx *chatContext) string {
	if chatCtx != nil && chatCtx.chat.MemoryRestricted() {
		return memoryExtractionDeveloperMessageNoScratchpad
	}
	return memoryExtractionDeveloperMessage
}

var memoryExtractionDeveloperMessage = `You can see the previous scratchpad in the earlier developer message and the updated scratchpad in the latest assistant message.
Use both, along with the recent conversation, to decide what should be written to long-term memory.
Prefer information that will remain useful after it falls out of the scratchpad.
Use the conversation and scratchpad context to decide what to store or how to phrase it. `

func (a *Agent) extractMemoriesWithScratchpadDelta(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, responseID *string, inferenceModelContext *provider.ModelContext, chatCtx *chatContext, compactionEventID *uuid.UUID) {
	if responseID == nil {
		a.logger.Error("response ID is nil. cannot extract memories from conversation.")
		return
	}
	prompt := memoryWritePromptText()

	instructions := chatCtx.chat.SystemPrompt
	params := responses.ResponseNewParams{
		Model:              archivalOpenAIModel,
		SafetyIdentifier:   openai.String(userID.String()),
		PreviousResponseID: openai.String(*responseID),
		MaxOutputTokens:    openai.Int(provider.DefaultMaxContentLength),
		ServiceTier:        responses.ResponseNewParamsServiceTierFlex,
		Instructions:       openai.String(instructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(
					fmt.Sprintf("%s\n\nDO NOT extract memories that would be duplicative of the following memories:\n\n %s", memoryExtractionDeveloperMessageFor(chatCtx), strings.Join(chatCtx.memories, "\n\n")), provider.RoleDeveloper),
				responses.ResponseInputItemParamOfMessage(prompt, provider.RoleUser),
			},
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:        "MemoryExtraction",
					Schema:      memoryExtractionSchema,
					Strict:      openai.Bool(true),
					Description: openai.String("Memory Extraction JSON"),
					Type:        "json_schema",
				},
			},
		},
	}

	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathMemory), params)
	if err != nil {
		a.logger.Error("failed to extract memories with scratchpad delta", zap.Error(err))
		return
	}

	extractedMemories := models.ExtractedMemoryResponse{}
	err = json.Unmarshal([]byte(resp.OutputText()), &extractedMemories)
	if err != nil {
		a.logger.Error("failed to unmarshal extracted memories", zap.Error(err))
		return
	}

	a.logger.Info("extracted memories (scratchpad delta)", zap.Any("count", len(extractedMemories.Memories)))
	a.compactMemoriesFromCheckpoint(ctx, userID, chatID, chatCtx.chat.PersonalityID, inferenceModelContext, chatCtx, extractedMemories.Memories, compactionEventID)
}

// extractMemoriesWithScratchpadDeltaClaude extracts long-term memories for Claude-model
// chats. It builds context from DB history instead of PreviousResponseID chaining, and
// makes the scratchpad delta (old → new) explicit via context blocks.
func (a *Agent) extractMemoriesWithScratchpadDeltaClaude(ctx context.Context, userID, chatID uuid.UUID, modelContext *provider.ModelContext, inferenceModelContext *provider.ModelContext, chatCtx *chatContext, compactionEventID *uuid.UUID) error {

	if a.ClaudeProvider == nil {
		return fmt.Errorf("ClaudeProvider is nil")
	}

	prompt := memoryWritePromptText()

	dedupNote := fmt.Sprintf("%s\n\nDO NOT extract memories that would be duplicative of the following memories:\n\n %s",
		memoryExtractionDeveloperMessageFor(chatCtx), strings.Join(chatCtx.memories, "\n\n"))

	modelContext.Append(provider.SegmentKindUserMessage, provider.RoleUser, prompt, false)
	modelContext.Append(provider.SegmentKindDeveloperContext, provider.RoleDeveloper, dedupNote, false)
	params := modelContext.BuildClaudeParams(archivalClaudeModel)
	// Mirror the OpenAI path: keep MaxTokens consistent with DefaultMaxContentLength.
	params.MaxTokens = int64(provider.DefaultMaxContentLength)
	// Structured JSON output (same schema as OpenAI memory extraction).
	params.OutputConfig = anthropic.OutputConfigParam{
		Format: anthropic.JSONOutputFormatParam{
			Type:   constant.JSONSchema("").Default(),
			Schema: memoryExtractionSchemaMapForClaude(),
		},
	}
	msg, err := a.ClaudeProvider.Call(telemetry.WithCallPath(ctx, telemetry.CallPathMemory), params)
	if err != nil {
		a.logger.Error("Claude memory extraction failed", zap.Error(err))
		return fmt.Errorf("Claude memory extraction failed: %w", err)
	}
	extractedMemories := models.ExtractedMemoryResponse{}
	if err := provider.UnmarshalClaudeTextJSON(msg, &extractedMemories); err != nil {
		a.logger.Error("failed to unmarshal Claude memory extraction", zap.Error(err))
		return fmt.Errorf("failed to unmarshal Claude memory extraction: %w", err)
	}

	a.logger.Info("extracted memories (Claude scratchpad delta)", zap.Int("count", len(extractedMemories.Memories)))
	a.compactMemoriesFromCheckpoint(ctx, userID, chatID, chatCtx.chat.PersonalityID, inferenceModelContext, chatCtx, extractedMemories.Memories, compactionEventID)

	return nil
}

func (a *Agent) getMemoryQuery(ctx context.Context, userID uuid.UUID, prompt string, userMessage string) (models.MemoryQuery, error) {

	params := responses.ResponseNewParams{
		Model:            "gpt-4.1-nano-2025-04-14",
		SafetyIdentifier: openai.String(userID.String()),
		Temperature:      openai.Float(provider.DefaultTemperature),
		MaxOutputTokens:  openai.Int(provider.DefaultMaxContentLength),
		Instructions:     openai.String(prompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userMessage),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:        "MemoryQuery",
					Schema:      memoryQuerySchema,
					Strict:      openai.Bool(true),
					Description: openai.String("Memory Query JSON"),
					Type:        "json_schema",
				},
			},
		},
	}

	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathMemory), params)
	if err != nil {
		a.logger.Error("failed to get memory query", zap.Error(err))
		return models.MemoryQuery{}, err
	}

	memoryQuery := models.MemoryQuery{}
	err = json.Unmarshal([]byte(resp.OutputText()), &memoryQuery)
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

// getMemories prefetches the memories relevant to userMessage for one turn. limit is the chat's
// memory sensitivity limit, applied in the retrieval SQL.
func (a *Agent) getMemories(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, personalityID uuid.UUID, userMessage string, limit models.MemorySensitivity) ([]string, []*models.Memory, error) {
	chatUser, err := a.ds.GetUserByID(ctx, userID)
	if err != nil {
		a.logger.Error("failed to get chat user", zap.Error(err))
		return nil, nil, err
	}
	userMessageCount, err := a.ds.GetChatMessageCount(ctx, userID, chatID, models.MessageOriginFilterUser)
	if err != nil {
		a.logger.Error("failed to get user message count", zap.Error(err))
		return nil, nil, err
	}
	memories := []string{}
	liveMemories := []*models.Memory{}
	// Only append the username to the first request, to not spam our context.
	if userMessageCount == 0 {
		memories = append(memories, userNameContextLine(chatUser.Username))
	}
	prompt := MemoryQueryPrompt
	memoryQuery, err := a.getMemoryQuery(ctx, userID, prompt, userMessage)
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

		dbMemories, err := a.ds.GetRelatedMemories(ctx, userID, chatID, memoryQueryEmbedding, personalityID, limit)
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
	restricted := chatCtx != nil && chatCtx.chat.MemoryRestricted()
	var liveMemories []*models.Memory
	if chatCtx != nil {
		liveMemories = chatCtx.liveMemories
	}
	if restricted {
		// A restricted chat is a sandbox a stranger may be talking in: what it extracts is kept to
		// this chat, and the merge may only touch memories this chat created (see
		// restrictedCompactionInputs). The datastore enforces the same rule on write.
		memories, liveMemories = restrictedCompactionInputs(chatID, memories, liveMemories)
		inferenceModelContext = nil // its memory refs are the owner's wider memories; none are ours to fold
	}
	collapsed := memoryutil.CollapseExtractedMemories(memories)

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
	if restricted {
		for i := range groups {
			groups[i].Scope = memoryScopeChat // the grouping model may propose User scope; a sandbox never writes it
		}
	}
	plan := planMemoryCompaction(groups, candidates)
	if chatCtx != nil {
		plan.SensitivityLimit = chatCtx.chat.MemoryLimit()
	}
	a.applyMemoryCompactionPlan(ctx, userID, chatID, activePersonalityID, plan, compactionEventID)
}

// memoryScopeChat is the Chat memory scope in the merge pipeline's vocabulary.
const memoryScopeChat = "Chat"

// restrictedCompactionInputs narrows a restricted chat's checkpoint inputs: every new extraction is
// forced to Chat scope (nothing a stranger says becomes an account-wide fact), and only loaded
// memories this chat created stay (the owner's and other chats' memories cannot be folded,
// rewritten or retired from here). The inputs are not mutated.
func restrictedCompactionInputs(chatID uuid.UUID, extracted []models.ExtractedMemory, live []*models.Memory) ([]models.ExtractedMemory, []*models.Memory) {
	scoped := make([]models.ExtractedMemory, 0, len(extracted))
	for _, m := range extracted {
		m.Scope = memoryScopeChat
		scoped = append(scoped, m)
	}
	own := make([]*models.Memory, 0, len(live))
	for _, mem := range live {
		if mem != nil && chatID != uuid.Nil && mem.ChatID == chatID {
			own = append(own, mem)
		}
	}
	return scoped, own
}

// foldMemberOptions carries a fold's new-member sensitivity to the datastore, capped by the
// asking chat's limit. A group with no new member passes nothing, leaving the stored members'
// levels to decide the survivor's. A restricted chat's fold is also confined to memories that chat
// created (datastore.WithChatMemoriesOnly).
func foldMemberOptions(fold memoryFoldPlan, limit models.MemorySensitivity) []datastore.MergeGroupOption {
	var opts []datastore.MergeGroupOption
	if limit.Restricted() {
		opts = append(opts, datastore.WithChatMemoriesOnly())
	}
	if level := foldNewMemberSensitivity(fold, limit); level != "" {
		opts = append(opts, datastore.WithNewMemberSensitivity(level))
	}
	return opts
}

// foldNewMemberSensitivity is the level a fold's new members are stored at: the level extraction
// gave, capped by the chat's limit; "" when the group has no new member.
func foldNewMemberSensitivity(fold memoryFoldPlan, limit models.MemorySensitivity) models.MemorySensitivity {
	if fold.NewSensitivity == "" {
		return ""
	}
	return models.CapToLimit(fold.NewSensitivity, limit)
}

// applyMemoryCompactionPlan embeds where needed and writes fold/link plans to the datastore.
// Decision logic (no-op guards, survivor selection, link eligibility) lives in planMemoryCompaction.
// Plans treat Group as immutable — do not mutate fold.Group / link.Group before persistence.
func (a *Agent) applyMemoryCompactionPlan(
	ctx context.Context,
	userID, chatID, activePersonalityID uuid.UUID,
	plan memoryCompactionPlan,
	compactionEventID *uuid.UUID,
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
			foldMemberOptions(fold, plan.SensitivityLimit)...,
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
				// New memories are capped by the chat's limit, so a restricted chat's writes stay readable there.
				Sensitivity: models.CapToLimit(member.Sensitivity, plan.SensitivityLimit),
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
			linkGroupOptions(plan.SensitivityLimit)...,
		); err != nil {
			a.logger.Error("failed to persist memory link group", zap.Error(err))
		}
	}
}

// linkGroupOptions are the datastore options for a link group written under limit: a restricted
// chat's link is confined to memories that chat created and writes Chat scope.
func linkGroupOptions(limit models.MemorySensitivity) []datastore.MergeGroupOption {
	if limit.Restricted() {
		return []datastore.MergeGroupOption{datastore.WithChatMemoriesOnly()}
	}
	return nil
}
