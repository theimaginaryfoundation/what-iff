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
