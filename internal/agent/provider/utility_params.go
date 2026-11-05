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
	if req.MaxOutputTokens > 0 {
		params.MaxOutputTokens = openai.Int(req.MaxOutputTokens)
	}
	switch {
	case req.Priority:
		params.ServiceTier = responses.ResponseNewParamsServiceTierPriority
	case req.Flex:
		params.ServiceTier = responses.ResponseNewParamsServiceTierFlex
	}
	if len(req.Messages) == 0 {
		params.Input = responses.ResponseNewParamsInputUnion{OfString: openai.String(req.Input)}
	} else {
		items := make([]responses.ResponseInputItemUnionParam, 0, len(req.Messages))
		for _, m := range req.Messages {
			items = append(items, utilityResponsesMessage(m))
		}
		params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: items}
	}
	params.Text = utilityResponsesText(req)
	return params
}

// utilityResponsesParamsFromContext renders a call that continues a
// conversation the way a chat turn on the model renders it, with the call's
// own messages after it. Without instructions, the conversation's system
// prompt stands.
func utilityResponsesParamsFromContext(model string, req UtilityRequest) responses.ResponseNewParams {
	params := req.Context.BuildOpenAIResponseParams(OpenAIResponseParamsOptions{
		Model:           model,
		SafetyUserID:    req.SafetyUserID,
		MaxOutputTokens: int(req.MaxOutputTokens),
		Instructions:    req.Instructions,
	})
	for _, m := range req.Messages {
		params.Input.OfInputItemList = append(params.Input.OfInputItemList, utilityResponsesMessage(m))
	}
	params.Text = utilityResponsesText(req)
	return params
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

func utilityResponsesMessage(m UtilityMessage) responses.ResponseInputItemUnionParam {
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
// Developer turns have no Messages role, so they join the instructions as
// system text, in order.
func UtilityClaudeParams(model string, req UtilityRequest) anthropic.MessageNewParams {
	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = int64(DefaultMaxContentLength)
	}
	var params anthropic.MessageNewParams
	var system []string
	if req.Context != nil {
		params = req.Context.BuildClaudeParamsWithMaxTokens(model, int(maxTokens))
		for _, b := range params.System {
			system = append(system, b.Text)
		}
		if req.Instructions != "" {
			system = append([]string{req.Instructions}, system...)
		}
	} else {
		params = anthropic.MessageNewParams{Model: anthropic.Model(model), MaxTokens: maxTokens}
		system = []string{req.Instructions}
	}
	for _, m := range utilityMessages(req) {
		if m.Role == RoleDeveloper {
			system = append(system, m.Text)
			continue
		}
		blocks := []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(m.Text)}
		if mediaType, data, ok := splitImageDataURL(m.ImageDataURL); ok {
			blocks = append(blocks, anthropic.NewImageBlockBase64(mediaType, data))
		}
		params.Messages = append(params.Messages, anthropic.NewUserMessage(blocks...))
	}
	if text := strings.TrimSpace(strings.Join(system, "\n\n")); text != "" {
		params.System = []anthropic.TextBlockParam{{Text: text}}
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
	if req.MaxOutputTokens > 0 {
		params.MaxCompletionTokens = openai.Int(req.MaxOutputTokens)
	}
	for _, m := range utilityMessages(req) {
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

// utilityMessages is the turns a request adds: Messages, or Input as its one
// user turn when there are no Messages and no conversation.
func utilityMessages(req UtilityRequest) []UtilityMessage {
	if len(req.Messages) > 0 || req.Context != nil {
		return req.Messages
	}
	return []UtilityMessage{{Role: RoleUser, Text: req.Input}}
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
