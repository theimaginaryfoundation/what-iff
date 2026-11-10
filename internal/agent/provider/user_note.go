package provider

import (
	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// UserNoteAppender is implemented by adapters that can add a user message right after the tool
// results just appended (AppendToolResults), before the next Call. A follow-up the person sends
// while the turn runs (steering, ADR 0x042) goes in this way, as their own turn rather than as
// text inside a tool's output. Every adapter here implements it; the agent falls back to folding
// the note into the last tool result for one that doesn't.
type UserNoteAppender interface {
	AppendUserNote(text string)
}

// AppendUserNote adds text as a user turn after the tool results. Anthropic merges consecutive
// user turns, as it already does for the tool-image turn appendClaudeToolResultImages adds.
func (a *ClaudeAdapter) AppendUserNote(text string) {
	if a.useBetaMCP {
		a.betaParams.Messages = append(a.betaParams.Messages, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(text)))
		return
	}
	a.params.Messages = append(a.params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(text)))
}

// AppendUserNote adds a user message item after the function-call outputs staged for the next
// request (AppendToolResults leaves them as the input list).
func (a *OpenAIAdapter) AppendUserNote(text string) {
	items := a.params.Input.OfInputItemList
	items = append(items, responses.ResponseInputItemParamOfMessage(text, RoleUser))
	a.params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: items}
}

// AppendUserNote adds a user message after the tool messages, as ForceFinalResponse does.
func (a *GeminiAdapter) AppendUserNote(text string) {
	a.params.Messages = append(a.params.Messages, openai.UserMessage(text))
}

// AppendUserNote adds a user message after the tool messages, as ForceFinalResponse does.
func (a *reasoningChatAdapter) AppendUserNote(text string) {
	a.params.Messages = append(a.params.Messages, openai.UserMessage(text))
}

// AppendUserNote adds a user message after the tool messages, as ForceFinalResponse does.
func (a *XiaomiAdapter) AppendUserNote(text string) {
	a.params.Messages = append(a.params.Messages, openai.UserMessage(text))
}

// AppendUserNote adds a user message after the tool messages, as ForceFinalResponse does.
func (a *LocalAdapter) AppendUserNote(text string) {
	a.params.Messages = append(a.params.Messages, openai.UserMessage(text))
}

// AppendUserNote records the note (UserNotes) and counts it toward later calls' input.
func (m *MockAdapter) AppendUserNote(text string) {
	m.userNotes = append(m.userNotes, text)
	m.grown += estimateMockTokens(text)
}

// UserNotes exposes the recorded AppendUserNote input for tests.
func (m *MockAdapter) UserNotes() []string { return m.userNotes }
