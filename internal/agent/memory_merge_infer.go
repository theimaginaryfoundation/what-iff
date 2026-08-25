package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"go.uber.org/zap"
)

// memoryMergeGroupingPrompt is the TASK instruction for the merge/link pass. It is sent
// as a developer message on top of the agent's own persona system prompt (see
// inferMemoryMergeGroupsOpenAI) so the same point of view that formed these memories decides
// how they fold — memory judgments made from a foreign POV misfire.
//
// Design priors baked in here (from the dedup design work):
//   - Weaken the similarity bar: match on FUNCTIONAL equivalence, not textual closeness. The
//     old prompt said "wording differs slightly", which under-merged obvious duplicates. Very
//     high textual similarity is not required to merge.
//   - AGGRESSIVENESS DIAL (currently medium, per tuning — the manual similarity tool liked a loose
//     ~0.4-0.6 cosine cutoff): the "bias STRONGLY toward merging" / "MERGE AGGRESSIVELY" language
//     below is the knob. Soften it if we see too much matching; the link carve-out below is what
//     stops aggression from eating related-but-distinct surfaces.
//   - merge vs link: true duplicates fold (merge); same-topic-different-function memories are
//     related-but-distinct and must be preserved as separate surfaces (link). Collapsing the
//     latter loses functional surface area.
//   - Repetition is corroboration: independent re-arrival at the same fact should RAISE
//     confidence, not lower it. Only downgrade when members actually disagree.
const memoryMergeGroupingPrompt = `You are reviewing your own long-term memory for consistency, in your own voice and point of view. You wrote (or would have written) these; decide which are really the same thing.

You receive a numbered list of memory candidates. Each has a scope tag (User = global, Chat = thread-specific) and may include an existing memory_id.

## Your task

Group candidates that are functionally equivalent — same underlying fact, preference, rule, or constraint — even when phrased differently or at different specificity. Bias toward merging; over-merges are cheap to undo, scattered redundancy is not.

For each cluster, choose ONE relation:

**merge** — Members assert the SAME fact. Fold into one canonical statement that preserves every load-bearing detail (IDs, channel names, exact rules, numbers). If unsure whether two are "same enough," prefer merge. Match on what the memory DOES, not what it's ABOUT — two memories about the same topic but serving different functions are link, not merge.

**link** — Members share a topic or event but serve DIFFERENT functions (technical lesson vs. emotional meaning vs. narrative vs. open question vs. metaphor). Each preserves a surface the others do not. Keep separate; cross-reference with a short shared label. This is the one case where similar-looking memories must NOT collapse.

## Rules

- Every candidate appears in exactly one group. Singletons use relation "merge".
- NEVER group across scopes. User and Chat are always separate groups.
- Do not group genuinely unrelated facts.
- Confidence: independent re-arrival at the same fact = corroboration → keep HIGHEST confidence. Only downgrade when members disagree or seem uncertain.
- Keep output compact: for any SINGLE-member group (a memory kept as-is), leave canonical_content EMPTY ("") — never echo the memory's text back. Only fill canonical_content when merging two or more members (the folded statement) or labeling a link.

Return JSON groups with member_indices, relation, canonical_content, scope, and confidence.`

var memoryMergeGroupingSchema = provider.GenerateSchema[models.MemoryMergeGroupingResponse]()

// memoryMergeMaxOutputTokens gives the grouping pass headroom well beyond DefaultMaxContentLength
// (8192). With ~100 candidates the JSON groups array can exceed 8k and truncate mid-array, which
// broke strict JSON parsing and failed the whole merge. Paired with empty canonical_content for
// singletons (see the prompt) and truncation salvage, this keeps the pass working at scale.
const memoryMergeMaxOutputTokens = 16384

type memoryMergeCandidate struct {
	Content    string
	Scope      string
	Confidence models.MemoryConfidence
	MemoryID   *uuid.UUID
	IsNew      bool
}

func dedupeContextMemoryRefs(refs []provider.ContextMemoryRef) []models.ContextMemoryRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]models.ContextMemoryRef, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		content := strings.TrimSpace(ref.Content)
		if content == "" {
			continue
		}
		scope := normalizeMemoryScope(ref.Scope)
		if scope == "" {
			continue
		}
		item := models.ContextMemoryRef{Content: content, Scope: scope}
		// Dedup by id when the ref carries one, so distinct duplicate rows (same content, different
		// ids) both survive to become merge candidates. Only id-less refs fall back to content+scope.
		var key string
		if idStr := strings.TrimSpace(ref.MemoryID); idStr != "" {
			if parsed, err := uuid.Parse(idStr); err == nil {
				item.MemoryID = &parsed
				key = "id\x00" + parsed.String()
			}
		}
		if key == "" {
			key = "cs\x00" + memoryutil.NormalizeContentForDedupe(memoryutil.StripMemoryContextMetadata(content)) + "\x00" + scope
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func buildMemoryMergeCandidates(modelContext *provider.ModelContext, liveMemories []*models.Memory, extracted []memoryutil.CollapsedExtractedMemory) []memoryMergeCandidate {
	contextRefs := dedupeContextMemoryRefs(nil)
	if modelContext != nil {
		contextRefs = dedupeContextMemoryRefs(modelContext.MemoryRefs)
	}
	candidates := make([]memoryMergeCandidate, 0, len(contextRefs)+len(liveMemories)+len(extracted))

	// Dedup so the SAME memory reachable through more than one path appears once, WITHOUT
	// collapsing genuinely distinct duplicate rows. modelContext.MemoryRefs is a snapshot frozen
	// before the turn's tool calls; memories pulled in later by find_context() land only in
	// chatCtx.liveMemories. We fold both in below.
	//
	// An id-bearing candidate is deduped ONLY against its own id: two distinct rows that share
	// normalized content are exactly the duplicate cluster the merger must SEE and fold, so they
	// must NOT collapse here. (In practice exact-content dupes are usually removed upstream when
	// memories are added to context; this keeps the merger correct if any slip through — near-dupes
	// with distinct wording already survive, since their content differs.) Content+scope dedup is
	// kept only for id-less candidates so an id-less echo of an already-added memory doesn't spawn a
	// spurious new merged row.
	seenID := make(map[string]struct{})
	seenContentScope := make(map[string]struct{})
	// markSeen always records a candidate's id (when present) and its content+scope, so a later
	// reference to the same row (or a later id-less echo of it) can be recognised.
	markSeen := func(id *uuid.UUID, content, scope string) {
		if id != nil && *id != uuid.Nil {
			seenID[id.String()] = struct{}{}
		}
		seenContentScope[memoryutil.NormalizeContentForDedupe(content)+"\x00"+scope] = struct{}{}
	}
	// alreadySeen reports whether a candidate is a REPEAT of one already added — the only thing we
	// skip. An id-bearing candidate is a repeat only when its exact id was already recorded (the same
	// row surfaced through two load paths); a not-yet-recorded id — including a distinct duplicate
	// row that shares content — is NOT a repeat and survives, so the merger can fold the cluster.
	// Only id-less candidates fall back to content+scope, so an id-less echo of an already-added
	// memory doesn't spawn a spurious new merged row.
	alreadySeen := func(id *uuid.UUID, content, scope string) bool {
		if id != nil && *id != uuid.Nil {
			_, isRepeatOfSameRow := seenID[id.String()]
			return isRepeatOfSameRow
		}
		_, isContentEcho := seenContentScope[memoryutil.NormalizeContentForDedupe(content)+"\x00"+scope]
		return isContentEcho
	}

	for _, ref := range contextRefs {
		content := memoryutil.StripMemoryContextMetadata(ref.Content)
		if strings.TrimSpace(content) == "" || ref.Scope == "" {
			continue
		}
		candidates = append(candidates, memoryMergeCandidate{
			Content:    content,
			Scope:      ref.Scope,
			Confidence: models.MemoryConfidenceMedium,
			MemoryID:   ref.MemoryID,
		})
		markSeen(ref.MemoryID, content, ref.Scope)
	}

	// find_context() results (and any other tool-loaded memories) accumulate in
	// chatCtx.liveMemories but are NOT in the frozen MemoryRefs snapshot. Functionally they are
	// identical to the prefetched context memories, so include them as candidates too — deduped by
	// id / content+scope against what we already added.
	for _, mem := range liveMemories {
		if mem == nil {
			continue
		}
		scope := normalizeMemoryScope(mem.Scope)
		content := strings.TrimSpace(mem.Content)
		if content == "" || scope == "" {
			continue
		}
		var idPtr *uuid.UUID
		if mem.ID != uuid.Nil {
			id := mem.ID
			idPtr = &id
		}
		if alreadySeen(idPtr, content, scope) {
			continue
		}
		candidates = append(candidates, memoryMergeCandidate{
			Content: content,
			Scope:   scope,
			// The pipeline reasons in buckets; the stored float is bucketed back for the candidate.
			Confidence: models.MemoryConfidenceFromFloat(mem.Confidence),
			MemoryID:   idPtr,
		})
		markSeen(idPtr, content, scope)
	}

	for _, item := range extracted {
		scope := normalizeMemoryScope(item.Scope)
		if scope == "" {
			continue
		}
		candidates = append(candidates, memoryMergeCandidate{
			Content:    item.Content,
			Scope:      scope,
			Confidence: item.Confidence,
			IsNew:      true,
		})
		for dup := 1; dup < item.BatchDuplicateCount; dup++ {
			candidates = append(candidates, memoryMergeCandidate{
				Content:    item.Content,
				Scope:      scope,
				Confidence: item.Confidence,
				IsNew:      true,
			})
		}
	}
	return candidates
}

func formatMemoryMergeCandidateList(candidates []memoryMergeCandidate) string {
	var b strings.Builder
	for i, c := range candidates {
		line := fmt.Sprintf("%d. %s", i, c.Content)
		if c.MemoryID != nil {
			line += fmt.Sprintf(" [memory_id=%s]", c.MemoryID.String())
		}
		if c.IsNew {
			line += " [new]"
		}
		if c.Scope != "" {
			line += fmt.Sprintf(" [scope=%s]", c.Scope)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func (a *Agent) inferMemoryMergeGroupsOpenAI(ctx context.Context, userID uuid.UUID, personaInstructions string, candidates []memoryMergeCandidate) ([]models.MemoryMergeGroupProposal, error) {
	if a.OpenAIProvider == nil {
		return nil, fmt.Errorf("OpenAIProvider is nil")
	}
	// POV consistency: the agent's own persona system prompt is the top-level instruction, so
	// the same self that formed these memories judges how they fold. The merge task rides as a
	// developer message. When no persona is available we fall back to the task prompt alone.
	instructions := strings.TrimSpace(personaInstructions)
	inputItems := make([]responses.ResponseInputItemUnionParam, 0, 2)
	if instructions == "" {
		instructions = memoryMergeGroupingPrompt
	} else {
		inputItems = append(inputItems, responses.ResponseInputItemParamOfMessage(memoryMergeGroupingPrompt, provider.RoleDeveloper))
	}
	inputItems = append(inputItems, responses.ResponseInputItemParamOfMessage(formatMemoryMergeCandidateList(candidates), provider.RoleUser))
	params := responses.ResponseNewParams{
		Model:            archivalOpenAIModel,
		SafetyIdentifier: openai.String(userID.String()),
		MaxOutputTokens:  openai.Int(memoryMergeMaxOutputTokens),
		ServiceTier:      responses.ResponseNewParamsServiceTierFlex,
		Instructions:     openai.String(instructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: inputItems,
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:        "MemoryMergeGrouping",
					Schema:      memoryMergeGroupingSchema,
					Strict:      openai.Bool(true),
					Description: openai.String("Memory merge grouping JSON"),
					Type:        "json_schema",
				},
			},
		},
	}
	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathMemory), params)
	if err != nil {
		return nil, err
	}
	text := resp.OutputText()
	var parsed models.MemoryMergeGroupingResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		// Strict JSON output does not survive truncation at MaxOutputTokens (a cut-off response is
		// still invalid JSON). Salvage every complete group object rather than losing the whole
		// pass; the fail-forward design tolerates the dropped tail (any candidate left uncovered is
		// exact-grouped by the caller, and re-merged on a later checkpoint).
		salvaged := salvageMemoryMergeGroups(text)
		if len(salvaged) == 0 {
			return nil, fmt.Errorf("parse memory merge grouping (output_len=%d): %w", len(text), err)
		}
		a.logger.Warn("memory merge grouping output was truncated; salvaged complete groups",
			zap.Int("salvaged_groups", len(salvaged)),
			zap.Int("output_len", len(text)))
		return salvaged, nil
	}
	return parsed.Groups, nil
}

// salvageMemoryMergeGroups recovers as many complete group objects as possible from a (possibly
// truncated) grouping response. The response is a single object {"groups":[ {...}, {...}, ... ]};
// we stream-decode array elements and stop at the first one that fails to parse (the truncated
// tail). Returns nil when nothing usable is present.
func salvageMemoryMergeGroups(raw string) []models.MemoryMergeGroupProposal {
	start := strings.IndexByte(raw, '[')
	if start < 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(raw[start:]))
	if _, err := dec.Token(); err != nil { // consume the opening '['
		return nil
	}
	var groups []models.MemoryMergeGroupProposal
	for dec.More() {
		var g models.MemoryMergeGroupProposal
		if err := dec.Decode(&g); err != nil {
			break // truncated / malformed element — keep everything before it
		}
		groups = append(groups, g)
	}
	return groups
}

func (a *Agent) inferMemoryMergeGroups(ctx context.Context, userID uuid.UUID, personaInstructions string, candidates []memoryMergeCandidate) []models.MemoryMergeGroupProposal {
	if len(candidates) <= 1 {
		return fallbackExactMergeGroups(candidates)
	}
	groups, err := a.inferMemoryMergeGroupsOpenAI(ctx, userID, personaInstructions, candidates)
	if err != nil {
		a.logger.Warn("memory merge grouping inference failed; falling back to exact match", zap.Error(err))
		return fallbackExactMergeGroups(candidates)
	}
	if len(groups) == 0 {
		return fallbackExactMergeGroups(candidates)
	}
	groups = filterGroupsByScopeConsistency(groups, candidates)
	// Singletons now arrive with empty canonical_content (prompt trims them to shrink output); fill
	// it from the member before planning, since persistence requires non-empty canonical content.
	backfillCanonicalContent(groups, candidates)
	// Completeness net: any candidate the model omitted or a truncation dropped is exact-grouped so
	// it is still processed. Fail-forward — this may create a duplicate that a later checkpoint
	// merges back, which is acceptable.
	return ensureAllCandidatesCovered(groups, candidates)
}

func fallbackExactMergeGroups(candidates []memoryMergeCandidate) []models.MemoryMergeGroupProposal {
	if len(candidates) == 0 {
		return nil
	}
	all := make([]int, len(candidates))
	for i := range candidates {
		all[i] = i
	}
	return exactMergeGroupsForIndices(candidates, all)
}

// exactMergeGroupsForIndices groups the given candidate indices into merge proposals keyed by
// normalized content + scope (an exact-duplicate fold with no LLM). Used as the offline fallback and
// to cover candidates the grouping inference left out.
func exactMergeGroupsForIndices(candidates []memoryMergeCandidate, indices []int) []models.MemoryMergeGroupProposal {
	if len(indices) == 0 {
		return nil
	}
	type bucket struct {
		indices []int
		scope   string
		conf    models.MemoryConfidence
	}
	buckets := make(map[string]*bucket)
	order := make([]string, 0)
	for _, i := range indices {
		if i < 0 || i >= len(candidates) {
			continue
		}
		c := candidates[i]
		key := memoryutil.NormalizeContentForDedupe(c.Content) + "\x00" + c.Scope
		b, ok := buckets[key]
		if !ok {
			b = &bucket{scope: c.Scope, conf: c.Confidence}
			buckets[key] = b
			order = append(order, key)
		}
		b.indices = append(b.indices, i)
		if memoryConfidenceRank[c.Confidence] < memoryConfidenceRank[b.conf] {
			b.conf = c.Confidence
		}
	}
	groups := make([]models.MemoryMergeGroupProposal, 0, len(order))
	for _, key := range order {
		b := buckets[key]
		groups = append(groups, models.MemoryMergeGroupProposal{
			MemberIndices:    append([]int(nil), b.indices...),
			Relation:         models.MemoryMergeRelationMerge,
			CanonicalContent: candidates[b.indices[0]].Content,
			Scope:            b.scope,
			Confidence:       b.conf,
		})
	}
	return groups
}

// backfillCanonicalContent fills empty canonical_content (which singletons now emit, to keep the
// grouping output small) from the group's first valid member, so downstream persistence — which
// drops empty-content groups — has real content to write.
func backfillCanonicalContent(groups []models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) {
	for i := range groups {
		if strings.TrimSpace(groups[i].CanonicalContent) != "" {
			continue
		}
		for _, idx := range groups[i].MemberIndices {
			if idx >= 0 && idx < len(candidates) && strings.TrimSpace(candidates[idx].Content) != "" {
				groups[i].CanonicalContent = candidates[idx].Content
				break
			}
		}
	}
}

// ensureAllCandidatesCovered appends exact-merge groups for any candidate not already referenced by
// a group, so a partial/truncated inference never silently drops a candidate (notably a freshly
// extracted memory that must still be created).
func ensureAllCandidatesCovered(groups []models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) []models.MemoryMergeGroupProposal {
	covered := make(map[int]struct{})
	for _, g := range groups {
		for _, idx := range g.MemberIndices {
			if idx >= 0 && idx < len(candidates) {
				covered[idx] = struct{}{}
			}
		}
	}
	var uncovered []int
	for i := range candidates {
		if _, ok := covered[i]; !ok {
			uncovered = append(uncovered, i)
		}
	}
	if len(uncovered) == 0 {
		return groups
	}
	return append(groups, exactMergeGroupsForIndices(candidates, uncovered)...)
}

var memoryConfidenceRank = map[models.MemoryConfidence]int{
	models.MemoryConfidenceLow:    0,
	models.MemoryConfidenceMedium: 1,
	models.MemoryConfidenceHigh:   2,
}

func filterGroupsByScopeConsistency(groups []models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) []models.MemoryMergeGroupProposal {
	out := make([]models.MemoryMergeGroupProposal, 0, len(groups))
	for _, group := range groups {
		groupScope := normalizeMemoryScope(group.Scope)
		if groupScope == "" {
			continue
		}
		valid := true
		for _, idx := range group.MemberIndices {
			if idx < 0 || idx >= len(candidates) {
				valid = false
				break
			}
			if candidates[idx].Scope != groupScope {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		group.Scope = groupScope
		out = append(out, group)
	}
	return out
}

// groupHasNewMember reports whether any member of the group is a freshly-extracted candidate
// (as opposed to a memory already loaded from storage). Used only as a no-op guard now that the
// merge trigger no longer *requires* a new member — see compactMemoriesFromCheckpoint.
func groupHasNewMember(group models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) bool {
	for _, idx := range group.MemberIndices {
		if idx < 0 || idx >= len(candidates) {
			continue
		}
		if candidates[idx].IsNew {
			return true
		}
	}
	return false
}

func survivorMemoryIDForGroup(group models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) (*uuid.UUID, []uuid.UUID) {
	var survivor *uuid.UUID
	absorb := make([]uuid.UUID, 0)
	groupScope := normalizeMemoryScope(group.Scope)
	for _, idx := range group.MemberIndices {
		if idx < 0 || idx >= len(candidates) {
			continue
		}
		candidate := candidates[idx]
		if candidate.Scope != groupScope {
			continue
		}
		id := candidate.MemoryID
		if id == nil {
			continue
		}
		if survivor == nil {
			survivor = id
			continue
		}
		if *id != *survivor {
			absorb = append(absorb, *id)
		}
	}
	return survivor, absorb
}

func sourceMembersForGroup(group models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) []models.MemoryMergeSourceMember {
	if len(group.MemberIndices) == 0 {
		return nil
	}
	members := make([]models.MemoryMergeSourceMember, 0, len(group.MemberIndices))
	for _, idx := range group.MemberIndices {
		if idx < 0 || idx >= len(candidates) {
			continue
		}
		candidate := candidates[idx]
		members = append(members, models.MemoryMergeSourceMember{
			Content:    candidate.Content,
			Scope:      candidate.Scope,
			Confidence: candidate.Confidence,
			MemoryID:   candidate.MemoryID,
			IsNew:      candidate.IsNew,
		})
	}
	return members
}

// memoryCompactionPlan is the pure outcome of classifying inferred groups against candidates.
// Embeddings and datastore writes are applied separately (see applyMemoryCompactionPlan).
//
// Invariants assumed of inputs (normally guaranteed by inferMemoryMergeGroups /
// filterGroupsByScopeConsistency):
//   - group.Scope is already normalized (User|Chat) when non-empty
//   - MemberIndices reference in-range candidate slots when valid; out-of-range / cross-scope
//     indices are skipped rather than panicked
//   - SourceMembers on each plan mirrors sourceMembersForGroup over the group's MemberIndices
//
// Plan structs treat Group as immutable after planning — do not mutate shared proposals in place.
type memoryCompactionPlan struct {
	Folds []memoryFoldPlan
	Links []memoryLinkPlan
}

// memoryFoldPlan is one consolidate/create action ready for PersistMemoryMergeGroup.
type memoryFoldPlan struct {
	Group            models.MemoryMergeGroupProposal
	SurvivorID       *uuid.UUID
	AbsorbIDs        []uuid.UUID
	DuplicatesFolded int
	NeedsEmbedding   bool
	SourceMembers    []models.MemoryMergeSourceMember
}

// memoryLinkNewMemberPlan is a freshly-extracted surface in a link cluster (embedding deferred).
type memoryLinkNewMemberPlan struct {
	Content    string
	Confidence models.MemoryConfidence
}

// memoryLinkPlan is one cross-reference action ready for PersistMemoryLinkGroup.
type memoryLinkPlan struct {
	Group         models.MemoryMergeGroupProposal
	Scope         string
	ExistingIDs   []uuid.UUID
	NewMembers    []memoryLinkNewMemberPlan
	SourceMembers []models.MemoryMergeSourceMember
}

// planMemoryCompaction partitions inferred groups into fold vs link actions and applies
// no-op / eligibility guards. Pure: no I/O, no embedding, no datastore mutation.
func planMemoryCompaction(groups []models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) memoryCompactionPlan {
	plan := memoryCompactionPlan{}
	for _, group := range groups {
		if group.Relation == models.MemoryMergeRelationLink {
			if link, ok := planLinkGroup(group, candidates); ok {
				plan.Links = append(plan.Links, link)
			}
			continue
		}
		if fold, ok := planFoldGroup(group, candidates); ok {
			plan.Folds = append(plan.Folds, fold)
		}
	}
	return plan
}

func planFoldGroup(group models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) (memoryFoldPlan, bool) {
	survivorID, absorbIDs := survivorMemoryIDForGroup(group, candidates)
	hasNew := groupHasNewMember(group, candidates)

	// No-op guard: a single already-stored memory with nothing to fold would otherwise
	// spuriously inflate its own duplicate_count. Require the group to actually consolidate.
	if survivorID != nil && len(absorbIDs) == 0 && !hasNew {
		return memoryFoldPlan{}, false
	}

	duplicatesFolded := len(group.MemberIndices)
	if duplicatesFolded < 1 {
		duplicatesFolded = 1
	}

	return memoryFoldPlan{
		Group:            group,
		SurvivorID:       survivorID,
		AbsorbIDs:        absorbIDs,
		DuplicatesFolded: duplicatesFolded,
		NeedsEmbedding:   survivorID == nil,
		SourceMembers:    sourceMembersForGroup(group, candidates),
	}, true
}

// planLinkGroup returns ok=false (drop / no-op) when the group has fewer than two valid,
// same-scope members after skipping out-of-range indices and cross-scope mismatches.
func planLinkGroup(group models.MemoryMergeGroupProposal, candidates []memoryMergeCandidate) (memoryLinkPlan, bool) {
	if len(group.MemberIndices) < 2 {
		return memoryLinkPlan{}, false
	}
	groupScope := normalizeMemoryScope(group.Scope)
	var existingIDs []uuid.UUID
	var newMembers []memoryLinkNewMemberPlan
	for _, idx := range group.MemberIndices {
		if idx < 0 || idx >= len(candidates) {
			continue
		}
		c := candidates[idx]
		if normalizeMemoryScope(c.Scope) != groupScope {
			continue
		}
		if c.MemoryID != nil {
			existingIDs = append(existingIDs, *c.MemoryID)
			continue
		}
		newMembers = append(newMembers, memoryLinkNewMemberPlan{
			Content:    c.Content,
			Confidence: c.Confidence,
		})
	}
	if len(existingIDs)+len(newMembers) < 2 {
		return memoryLinkPlan{}, false
	}
	return memoryLinkPlan{
		Group:         group,
		Scope:         groupScope,
		ExistingIDs:   existingIDs,
		NewMembers:    newMembers,
		SourceMembers: sourceMembersForGroup(group, candidates),
	}, true
}
