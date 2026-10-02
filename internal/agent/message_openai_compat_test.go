package agent

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestOpenAIChatCompletionsAdapter_SelectsProvider(t *testing.T) {
	t.Parallel()

	a := &Agent{
		logger:           zap.NewNop(),
		MistralProvider:  provider.NewMistralProvider("mistral-key", "", nil, nil),
		DeepSeekProvider: provider.NewDeepSeekProvider("deepseek-key", "", nil, nil),
		QwenProvider:     provider.NewQwenProvider("qwen-key", "", nil, nil),
		XiaomiProvider:   provider.NewXiaomiProvider("xiaomi-key", "", nil, nil),
	}

	params := openai.ChatCompletionNewParams{Model: shared.ChatModel("mistral-large-latest")}

	adapter, err := a.openAIChatCompletionsAdapter(
		&chatContext{modelProvider: "mistral", model: "mistral-large-latest"},
		params,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, adapter)

	_, err = a.openAIChatCompletionsAdapter(
		&chatContext{modelProvider: "deepseek", model: "deepseek-chat"},
		openai.ChatCompletionNewParams{Model: shared.ChatModel("deepseek-chat")},
		nil,
		nil,
	)
	require.NoError(t, err)

	_, err = a.openAIChatCompletionsAdapter(
		&chatContext{modelProvider: "qwen", model: "qwen-plus"},
		openai.ChatCompletionNewParams{Model: shared.ChatModel("qwen-plus")},
		nil,
		nil,
	)
	require.NoError(t, err)

	_, err = a.openAIChatCompletionsAdapter(
		&chatContext{modelProvider: "xiaomi", model: "mimo-v2.5-pro"},
		openai.ChatCompletionNewParams{Model: shared.ChatModel("mimo-v2.5-pro")},
		nil,
		nil,
	)
	require.NoError(t, err)

	_, err = a.openAIChatCompletionsAdapter(
		&chatContext{modelProvider: string(models.ModelProviderGoogle), model: "gemini-3.5"},
		openai.ChatCompletionNewParams{Model: shared.ChatModel("gemini-3.5")},
		nil,
		nil,
	)
	require.Error(t, err)
}

func TestOpenAIChatCompletionsAdapter_MissingAPIKey(t *testing.T) {
	t.Parallel()

	a := &Agent{logger: zap.NewNop()}
	_, err := a.openAIChatCompletionsAdapter(
		&chatContext{modelProvider: "mistral", model: "mistral-large-latest"},
		openai.ChatCompletionNewParams{Model: shared.ChatModel("mistral-large-latest")},
		nil,
		nil,
	)
	require.ErrorContains(t, err, "MISTRAL_API_KEY")
}

// userImageParts returns the image_url parts of every user message in params.
func userImageParts(params openai.ChatCompletionNewParams) []openai.ChatCompletionContentPartImageParam {
	var out []openai.ChatCompletionContentPartImageParam
	for _, msg := range params.Messages {
		if msg.OfUser == nil {
			continue
		}
		for _, part := range msg.OfUser.Content.OfArrayOfContentParts {
			if part.OfImageURL != nil {
				out = append(out, *part.OfImageURL)
			}
		}
	}
	return out
}

// Regression for #143: a vision model's rendered Chat Completions request keeps the
// user's image as an OpenAI-style image_url part; a text-only model's is stripped.
// The gate is the model row's vision_support flag, not the model id.
func TestBuildOpenAIChatCompletionsParams_ImageGating(t *testing.T) {
	t.Parallel()

	newCtx := func() *provider.ModelContext {
		mc := &provider.ModelContext{}
		mc.Append(provider.SegmentKindSystemPrompt, provider.RoleDeveloper, "sys", true)
		mc.AppendUserMessage(provider.RoleUser, "what is in this picture?", []provider.UserMessageImage{
			{RawBytes: []byte{0x89, 0x50, 0x4e, 0x47}, MediaType: "image/png"},
		}, false)
		return mc
	}

	tests := []struct {
		provider   string
		model      string
		wantImages bool
	}{
		{provider: "xiaomi", model: "mimo-v2.6", wantImages: true},
		{provider: "xiaomi", model: "mimo-v2.5-pro", wantImages: false},
		{provider: "deepseek", model: "deepseek-chat", wantImages: false},
		// Flag wins over the id: a DeepSeek row marked vision-capable keeps images.
		{provider: "deepseek", model: "deepseek-vl", wantImages: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			t.Parallel()
			mc := newCtx()
			params := buildOpenAIChatCompletionsParams(&chatContext{modelProvider: tt.provider, model: tt.model, modelVisionSupport: tt.wantImages}, mc)

			require.Equal(t, shared.ChatModel(tt.model), params.Model)
			parts := userImageParts(params)
			if tt.wantImages {
				require.Len(t, parts, 1)
				require.Equal(t, "data:image/png;base64,iVBORw==", parts[0].ImageURL.URL)
			} else {
				require.Empty(t, parts)
			}
			// The source context is never mutated by rendering.
			require.Len(t, mc.Segments[1].UserImages, 1)
		})
	}
}

func TestVisionRenderContext(t *testing.T) {
	t.Parallel()
	mc := &provider.ModelContext{}
	mc.AppendUserMessage(provider.RoleUser, "", []provider.UserMessageImage{
		{RawBytes: []byte{0x89}, MediaType: "image/png"},
	}, false)

	require.Same(t, mc, visionRenderContext(&chatContext{modelVisionSupport: true}, mc))

	textOnly := visionRenderContext(&chatContext{modelVisionSupport: false}, mc)
	require.NotSame(t, mc, textOnly)
	require.Empty(t, textOnly.Segments[0].UserImages)
	require.Equal(t, provider.TextOnlyImageFallback, textOnly.Segments[0].Content)
	require.Len(t, mc.Segments[0].UserImages, 1, "source context is not mutated")
}
