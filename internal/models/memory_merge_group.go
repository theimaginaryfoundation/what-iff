package models

import "github.com/google/uuid"

// ContextMemoryRef is one memory snippet already present in the active thread model context.
type ContextMemoryRef struct {
	MemoryID *uuid.UUID `json:"memory_id,omitempty"`
	Content  string     `json:"content"`
	Scope    string     `json:"scope,omitempty"`
}

// MemoryMergeRelation is how the members of a proposed group relate to one another.
type MemoryMergeRelation string

const (
	// MemoryMergeRelationMerge folds members into one canonical memory — true duplicates
	// that express the same durable fact/preference/rule.
	MemoryMergeRelationMerge MemoryMergeRelation = "merge"
	// MemoryMergeRelationLink keeps members as distinct surfaces but records their
	// relatedness. Used for same-topic/same-event memories that serve DIFFERENT functions
	// (e.g. technical insight vs emotional meaning vs narrative vs open question vs metaphor).
	// Collapsing these would lose functional surface area, so we cross-reference instead.
	MemoryMergeRelationLink MemoryMergeRelation = "link"
)

// MemoryMergeGroupProposal is one related cluster from the merge-grouping inference pass.
// A cluster is either folded (merge) or cross-referenced (link) depending on Relation.
type MemoryMergeGroupProposal struct {
	MemberIndices    []int               `json:"member_indices" jsonschema_description:"Zero-based indices into the numbered candidate list"`
	Relation         MemoryMergeRelation `json:"relation" jsonschema:"enum=merge,enum=link" jsonschema_description:"merge = true duplicates to fold into one canonical memory; link = related-but-distinct surfaces to keep separate and cross-reference"`
	CanonicalContent string              `json:"canonical_content" jsonschema_description:"For a MERGE of two or more members: the canonical wording preserving every load-bearing detail. For a LINK: a short shared label. For a SINGLE-member group (a memory kept as-is): leave EMPTY — do not echo the memory's text back (it is derived from the member)."`
	Scope            string              `json:"scope" jsonschema:"enum=User,enum=Chat" jsonschema_description:"User for global memories, Chat for thread-specific"`
	Confidence       MemoryConfidence    `json:"confidence" jsonschema:"enum=low,enum=medium,enum=high"`
}

type MemoryMergeGroupingResponse struct {
	Groups []MemoryMergeGroupProposal `json:"groups" jsonschema_description:"Each candidate index must appear in exactly one group"`
}
