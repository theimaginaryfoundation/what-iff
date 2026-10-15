package provider

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

var pngImage = UserMessageImage{RawBytes: []byte{0x89, 'P', 'N', 'G'}, MediaType: "image/png"}

func TestStripResponsesInputImages(t *testing.T) {
	t.Parallel()
	var parts responses.ResponseInputMessageContentListParam
	parts = append(parts, responses.ResponseInputContentParamOfInputText("look at this"))
	parts = appendOpenAIResponseInputImages(parts, []UserMessageImage{pngImage, pngImage})
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("plain text", RoleUser),
		responses.ResponseInputItemParamOfMessage(parts, RoleUser),
		responses.ResponseInputItemParamOfFunctionCallOutput("call-1", "done"),
	}

	require.Equal(t, 2, stripResponsesInputImages(items))

	got := items[1].OfMessage.Content.OfInputItemContentList
	require.Len(t, got, 2)
	require.Equal(t, "look at this", got[0].OfInputText.Text)
	require.Equal(t, WithheldImageNote, got[1].OfInputText.Text, "the model is told an image was there")
	require.Equal(t, 0, stripResponsesInputImages(items), "a second strip finds nothing")
}

func TestOpenAIAdapterStripsOnlyWhatItIsAboutToSend(t *testing.T) {
	t.Parallel()
	a := NewOpenAIAdapter(nil, responses.ResponseNewParams{})
	a.previousResponseID = "resp_1"
	a.AppendToolResults([]ToolResult{{ID: "call-1", Output: "generated", Images: []UserMessageImage{pngImage}}})

	require.Equal(t, 1, a.StripImages(), "the generated image staged with the tool result")
	require.Equal(t, 0, a.StripImages())
}

func TestStripChatCompletionImages(t *testing.T) {
	t.Parallel()
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("be nice"),
		openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
			openai.TextContentPart("what is this?"),
			openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: "data:image/png;base64,AAAA"}),
		}),
		openai.UserMessage("text only"),
	}

	require.Equal(t, 1, stripChatCompletionImages(messages))

	parts := messages[1].OfUser.Content.OfArrayOfContentParts
	require.Len(t, parts, 2)
	require.Nil(t, parts[0].OfImageURL)
	require.Nil(t, parts[1].OfImageURL)
	require.Equal(t, WithheldImageNote, parts[1].OfText.Text)
}

func TestStripClaudeImages(t *testing.T) {
	t.Parallel()
	toolResult := anthropic.NewToolResultBlock("call-1", "drew it", false)
	toolResult.OfToolResult.Content = append(toolResult.OfToolResult.Content, anthropic.ToolResultBlockParamContentUnion{
		OfImage: &anthropic.ImageBlockParam{Source: anthropic.ImageBlockParamSourceUnion{OfBase64: &anthropic.Base64ImageSourceParam{Data: "AAAA", MediaType: "image/png"}}},
	})
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		anthropic.NewUserMessage(toolResult),
		anthropic.NewUserMessage(claudeImageBlocksFromUserImages([]UserMessageImage{pngImage}, GeneratedToolImageCaption)...),
	}

	require.Equal(t, 2, stripClaudeImages(messages), "one inside the tool result, one in the follow-up message")

	require.Len(t, messages[0].Content, 1, "untouched")
	require.Len(t, messages[1].Content[0].OfToolResult.Content, 1, "only the tool result's text is left")
	last := messages[2].Content
	require.Equal(t, GeneratedToolImageCaption, last[0].OfText.Text)
	require.Equal(t, WithheldImageNote, last[len(last)-1].OfText.Text)
	for _, b := range last {
		require.Nil(t, b.OfImage)
	}
}

func TestStripClaudeBetaImages(t *testing.T) {
	t.Parallel()
	messages := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(claudeBetaImageBlocksFromUserImages([]UserMessageImage{pngImage, pngImage}, "")...),
	}

	require.Equal(t, 2, stripClaudeBetaImages(messages))
	require.Len(t, messages[0].Content, 1)
	require.Equal(t, WithheldImageNote, messages[0].Content[0].OfText.Text)
}

// Every adapter that can carry images implements ImageStripper.
func TestAdaptersImplementImageStripper(t *testing.T) {
	t.Parallel()
	for name, adapter := range map[string]any{
		"openai":   &OpenAIAdapter{},
		"claude":   &ClaudeAdapter{},
		"gemini":   &GeminiAdapter{},
		"qwen":     &QwenAdapter{},
		"mistral":  &MistralAdapter{},
		"deepseek": &DeepSeekAdapter{},
		"xiaomi":   &XiaomiAdapter{},
		"local":    &LocalAdapter{},
	} {
		_, ok := adapter.(ImageStripper)
		require.True(t, ok, name)
	}
}
