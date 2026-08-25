package search

// Resource type identifiers used in the request `types` query param and the
// response `sections[].type` field. Kept as a small closed set so the handler
// can reject unknown values up front instead of leaking parsing surprises into
// the resource-specific fan-out code.
const (
	TypeChat        = "chat"
	TypePersonality = "personality"
	TypeRitual      = "ritual"
	TypeMemory      = "memory"
	TypeImage       = "image"
)

// AllTypes is the canonical, ordered set of supported resource types. The
// response always emits sections in this order so clients can render a
// predictable layout regardless of which sections matched.
var AllTypes = []string{
	TypeChat,
	TypePersonality,
	TypeRitual,
	TypeMemory,
	TypeImage,
}

// SearchResult is one entry inside a section. Fields are intentionally small
// (label/description/snippet trimmed by the handler) to keep the aggregate
// response well under a typical command-palette payload budget.
type SearchResult struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Route       string `json:"route"`
	IconType    string `json:"icon_type"`
	Score       int    `json:"score"`
	Snippet     string `json:"snippet,omitempty"`
}

// SearchSection groups results by resource type. Sections are always present
// in the response (with an empty Results slice when nothing matched) so the
// client renders consistent headers without a "missing key" branch.
type SearchSection struct {
	Type    string         `json:"type"`
	Results []SearchResult `json:"results"`
}

// SearchResponse is the top-level body returned by GET /search.
type SearchResponse struct {
	Query    string          `json:"query"`
	Sections []SearchSection `json:"sections"`
}
