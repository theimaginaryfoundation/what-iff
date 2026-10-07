package models

type ExtractedMemory struct {
	Content    string           `json:"content" jsonschema_description:"The content of the memory"`
	Scope      string           `json:"scope" jsonschema_description:"The scope of the memory. e.g. 'User' or 'Chat'" jsonschema:"enum=User,enum=Chat"`
	Confidence MemoryConfidence `json:"confidence" jsonschema_description:"How confident you are that this memory is factually correct and durable." jsonschema:"enum=low,enum=medium,enum=high"`
	// Speaker is set only by the relay-thread extraction variant (ExtractedRelayMemory): the
	// display name of the Discord user the memory came from. It is not part of the default schema.
	Speaker string `json:"-"`
}

type ExtractedMemoryResponse struct {
	Memories []ExtractedMemory `json:"memories"`
}

// ExtractedRelayMemory is the extraction schema variant used in a Discord relay thread,
// where the people talking are Discord users labelled by name ("alice (Discord, #general): ..."),
// not the account owner. It adds the speaker each memory came from.
type ExtractedRelayMemory struct {
	Content    string           `json:"content" jsonschema_description:"The content of the memory"`
	Scope      string           `json:"scope" jsonschema_description:"The scope of the memory. e.g. 'User' or 'Chat'" jsonschema:"enum=User,enum=Chat"`
	Confidence MemoryConfidence `json:"confidence" jsonschema_description:"How confident you are that this memory is factually correct and durable." jsonschema:"enum=low,enum=medium,enum=high"`
	Speaker    string           `json:"speaker" jsonschema_description:"The display name of the Discord user this memory came from, exactly as labelled before '(Discord' in their message. Use an empty string when it is unclear or the memory is your own observation."`
}

// ExtractedRelayMemoryResponse is the response of the relay-thread extraction variant.
type ExtractedRelayMemoryResponse struct {
	Memories []ExtractedRelayMemory `json:"memories"`
}

// ToExtracted converts the relay variant to ExtractedMemory, keeping the speaker.
func (r ExtractedRelayMemoryResponse) ToExtracted() []ExtractedMemory {
	out := make([]ExtractedMemory, 0, len(r.Memories))
	for _, m := range r.Memories {
		out = append(out, ExtractedMemory{
			Content:    m.Content,
			Scope:      m.Scope,
			Confidence: m.Confidence,
			Speaker:    m.Speaker,
		})
	}
	return out
}

type MemoryQuery struct {
	Query        string `json:"query" jsonschema_description:"The query to search for memories"`
	ShouldEnrich bool   `json:"should_enrich" jsonschema_description:"Whether to message the message context with the memories based on the user message"`
}
