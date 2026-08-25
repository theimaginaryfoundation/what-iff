package models

type UsageStats struct {
	Chats        int64 `json:"chats"`
	Messages     int64 `json:"messages"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
