package models

type ExtractedMemory struct {
	Content    string           `json:"content" jsonschema_description:"The content of the memory"`
	Scope      string           `json:"scope" jsonschema_description:"The scope of the memory. e.g. 'User' or 'Chat'" jsonschema:"enum=User,enum=Chat"`
	Confidence MemoryConfidence `json:"confidence" jsonschema_description:"How confident you are that this memory is factually correct and durable." jsonschema:"enum=low,enum=medium,enum=high"`
}

type ExtractedMemoryResponse struct {
	Memories []ExtractedMemory `json:"memories"`
}

type MemoryQuery struct {
	Query        string `json:"query" jsonschema_description:"The query to search for memories"`
	ShouldEnrich bool   `json:"should_enrich" jsonschema_description:"Whether to message the message context with the memories based on the user message"`
}
