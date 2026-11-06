package provider

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// UtilityResponsesParams renders a utility call for the OpenAI Responses API.
func UtilityResponsesParams(model string, req UtilityRequest) responses.ResponseNewParams {
	if req.Context != nil {
		return utilityResponsesParamsFromContext(model, req)
	}
	params := responses.ResponseNewParams{
		Model:        model,
		Instructions: openai.String(req.Instructions),
	}
	if req.SafetyUserID != "" {
		params.SafetyIdentifier = openai.String(req.SafetyUserID)
	}
	if req.PreviousResponseID != "" {
		params.PreviousResponseID = openai.String(req.PreviousResponseID)
	}
	if req.MaxOutputTokens > 0 {
		params.MaxOutputTokens = openai.Int(req.MaxOutputTokens)
	}
	params.ServiceTier = utilityServiceTier(req)
	if len(req.History) == 0 && req.InputImageDataURL == "" {
		params.Input = responses.ResponseNewParamsInputUnion{OfString: openai.String(req.Input)}
	} else {
		turns := utilityTurns(req)
		items := make([]responses.ResponseInputItemUnionParam, 0, len(turns))
		for _, t := range turns {
			items = append(items, utilityResponsesMessage(t))
		}
		params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: items}
	}
	params.Text = utilityResponsesText(req)
	return params
}

// utilityResponsesParamsFromContext renders a call that continues a
// conversation the way a chat turn on the model renders it, with the call's
// own History and Input after it. Without instructions, the conversation's system
// prompt stands. Images the Responses API rejects are dropped, since the
// conversation may come from a chat on another provider.
func utilityResponsesParamsFromContext(model string, req UtilityRequest) responses.ResponseNewParams {
	params := SanitizeImagesForOpenAIInput(req.Context).BuildOpenAIResponseParams(OpenAIResponseParamsOptions{
		Model:           model,
		SafetyUserID:    req.SafetyUserID,
		MaxOutputTokens: int(req.MaxOutputTokens),
		Instructions:    req.Instructions,
	})
	for _, t := range utilityTurns(req) {
		params.Input.OfInputItemList = append(params.Input.OfInputItemList, utilityResponsesMessage(t))
	}
	params.ServiceTier = utilityServiceTier(req)
	params.Text = utilityResponsesText(req)
	return params
}

// utilityServiceTier is the Responses service tier a request asks for, if any.
func utilityServiceTier(req UtilityRequest) responses.ResponseNewParamsServiceTier {
	switch req.ServiceTier {
	case UtilityServiceTierPriority:
		return responses.ResponseNewParamsServiceTierPriority
	case UtilityServiceTierFlex:
		return responses.ResponseNewParamsServiceTierFlex
	}
	return ""
}

// utilityResponsesText asks for the request's structured output, if any.
func utilityResponsesText(req UtilityRequest) responses.ResponseTextConfigParam {
	if req.Schema == nil {
		return responses.ResponseTextConfigParam{}
	}
	return responses.ResponseTextConfigParam{
		Format: responses.ResponseFormatTextConfigUnionParam{
			OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
				Name:        req.Schema.Name,
				Schema:      req.Schema.Schema,
				Strict:      openai.Bool(true),
				Description: openai.String(req.Schema.Description),
				Type:        "json_schema",
			},
		},
	}
}

func utilityResponsesMessage(m utilityTurn) responses.ResponseInputItemUnionParam {
	role := responses.EasyInputMessageRole(m.Role)
	if m.ImageDataURL == "" {
		return responses.ResponseInputItemParamOfMessage(m.Text, role)
	}
	parts := responses.ResponseInputMessageContentListParam{
		responses.ResponseInputContentParamOfInputText(m.Text),
		{OfInputImage: &responses.ResponseInputImageParam{
			ImageURL: openai.String(m.ImageDataURL),
			Detail:   responses.ResponseInputImageDetailAuto,
		}},
	}
	return responses.ResponseInputItemParamOfMessage(parts, role)
}

// UtilityClaudeParams renders a utility call for the Anthropic Messages API.
// Developer turns have no Messages role, so they follow the instructions as
// system blocks, in order. A conversation keeps its own system blocks, cache
// markers included, after the instructions.
func UtilityClaudeParams(model string, req UtilityRequest) anthropic.MessageNewParams {
	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = int64(DefaultMaxContentLength)
	}
	var params anthropic.MessageNewParams
	if req.Context != nil {
		params = req.Context.BuildClaudeParamsWithMaxTokens(model, int(maxTokens))
	} else {
		params = anthropic.MessageNewParams{Model: anthropic.Model(model), MaxTokens: maxTokens}
	}
	if req.Instructions != "" {
		params.System = append([]anthropic.TextBlockParam{{Text: req.Instructions}}, params.System...)
	}
	for _, m := range utilityTurns(req) {
		if m.Role == RoleDeveloper {
			params.System = append(params.System, anthropic.TextBlockParam{Text: m.Text})
			continue
		}
		blocks := []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(m.Text)}
		if mediaType, data, ok := splitImageDataURL(m.ImageDataURL); ok {
			blocks = append(blocks, anthropic.NewImageBlockBase64(mediaType, data))
		}
		params.Messages = append(params.Messages, anthropic.NewUserMessage(blocks...))
	}
	if req.Schema != nil {
		params.OutputConfig = anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema.Schema},
		}
	}
	return params
}

// UtilityChatCompletionParams renders a utility call for an OpenAI-compatible
// Chat Completions API. Instructions and developer turns are system messages.
func UtilityChatCompletionParams(model string, req UtilityRequest) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(model),
	}
	if req.Instructions != "" {
		params.Messages = append(params.Messages, openai.SystemMessage(req.Instructions))
	}
	if req.Context != nil {
		params.Messages = append(params.Messages, req.Context.BuildOpenAIChatCompletionParams(model).Messages...)
	}
	// Like the Responses API, and unlike Messages, which requires one, the cap is sent only
	// when the call sets it.
	if req.MaxOutputTokens > 0 {
		params.MaxCompletionTokens = openai.Int(req.MaxOutputTokens)
	}
	for _, m := range utilityTurns(req) {
		if m.Role == RoleDeveloper {
			params.Messages = append(params.Messages, openai.SystemMessage(m.Text))
			continue
		}
		if m.ImageDataURL == "" {
			params.Messages = append(params.Messages, openai.UserMessage(m.Text))
			continue
		}
		params.Messages = append(params.Messages, openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
			openai.TextContentPart(m.Text),
			openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: m.ImageDataURL}),
		}))
	}
	if req.Schema != nil {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        req.Schema.Name,
					Description: openai.String(req.Schema.Description),
					Schema:      req.Schema.Schema,
					Strict:      openai.Bool(true),
				},
			},
		}
	}
	return params
}

// utilityTurn is one turn a request adds after its conversation.
type utilityTurn struct {
	Role         UtilityMessageRole
	Text         string
	ImageDataURL string
}

// utilityTurns is the turns a request adds after its conversation: the History, then Input.
func utilityTurns(req UtilityRequest) []utilityTurn {
	turns := make([]utilityTurn, 0, len(req.History)+1)
	for _, m := range req.History {
		turns = append(turns, utilityTurn{Role: m.Role, Text: m.Text})
	}
	return append(turns, utilityTurn{Role: UtilityMessageUser, Text: req.Input, ImageDataURL: req.InputImageDataURL})
}

// splitImageDataURL splits "data:<media type>;base64,<data>".
func splitImageDataURL(u string) (mediaType, data string, ok bool) {
	rest, found := strings.CutPrefix(u, "data:")
	if !found {
		return "", "", false
	}
	header, data, found := strings.Cut(rest, ",")
	if !found {
		return "", "", false
	}
	mediaType, found = strings.CutSuffix(header, ";base64")
	return mediaType, data, found && mediaType != ""
}
