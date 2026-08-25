package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

var claudeUnsupportedSchemaKeys = map[string]struct{}{
	"minimum":  {},
	"maximum":  {},
	"maxItems": {},
	"default":  {},
}

// claudeToolName extracts the name from a Claude ToolUnionParam (function tools only).
func claudeToolName(t anthropic.ToolUnionParam) string {
	if t.OfTool != nil {
		return t.OfTool.Name
	}
	return ""
}

// claudeWebSearchTool adds Anthropic's native web search capability.
var claudeWebSearchTool = anthropic.ToolUnionParam{
	OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{},
}

// ClaudeFunctionTool builds an Anthropic function tool param from a provider-neutral
// function spec. Tool selection stays in the agent layer; this only handles SDK shape.
func ClaudeFunctionTool(name, description string, properties map[string]interface{}, required []string, strict bool) anthropic.ToolUnionParam {
	sanitizedProperties := sanitizeClaudeSchemaProperties(properties)

	return anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        name,
			Description: anthropic.String(description),
			Strict:      anthropic.Bool(strict),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: sanitizedProperties,
				Required:   required,
				ExtraFields: map[string]any{
					"additionalProperties": false,
				},
			},
		},
	}
}

func sanitizeClaudeSchemaProperties(properties map[string]interface{}) map[string]interface{} {
	sanitized, _ := sanitizeClaudeSchemaValue(properties).(map[string]interface{})
	return sanitized
}

func sanitizeClaudeSchemaValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, inner := range val {
			if _, unsupported := claudeUnsupportedSchemaKeys[k]; unsupported {
				continue
			}
			out[k] = sanitizeClaudeSchemaValue(inner)
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(val))
		for k, inner := range val {
			out[k] = inner
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(val))
		for _, inner := range val {
			out = append(out, sanitizeClaudeSchemaValue(inner))
		}
		return out
	case []string:
		out := make([]interface{}, 0, len(val))
		for _, inner := range val {
			out = append(out, inner)
		}
		return out
	default:
		return val
	}
}

// ClaudeAdapter implements AgentAdapter for the Anthropic Messages API.
// It is stateful: the params.Messages slice grows as tool-result turns are
// appended between rounds, mirroring the full conversation for each API call.
type ClaudeAdapter struct {
	provider   *ClaudeProvider
	params     anthropic.MessageNewParams
	useBetaMCP bool
	betaParams anthropic.BetaMessageNewParams
	initErr    error

	// webSearchCompleted counts assistant-side web_search_tool_result blocks seen this turn.
	webSearchCompleted int
	rawMessages        []*anthropic.Message
	rawBetaMessages    []*anthropic.BetaMessage
	textDeltaHandler   func(delta string)
}

// ClaudeMCPConfig contains Anthropic-beta MCP server definitions and corresponding toolsets.
type ClaudeMCPConfig struct {
	Servers  []anthropic.BetaRequestMCPServerURLDefinitionParam
	Toolsets []anthropic.BetaToolUnionParam
}

// NewClaudeAdapter constructs a ClaudeAdapter from pre-built MessageNewParams.
// It appends the standard agent tool list and, when toolsEnabled, the native
// web-search tool.
// disabledTools, if non-nil, removes tools whose names appear in the set.
// The caller is responsible for system, model, and message history in params.
func NewClaudeAdapter(provider *ClaudeProvider, params anthropic.MessageNewParams, functionTools []anthropic.ToolUnionParam, webSearchEnabled bool, mcpConfig *ClaudeMCPConfig, disabledTools map[string]bool) *ClaudeAdapter {
	for _, t := range functionTools {
		if disabledTools[claudeToolName(t)] {
			continue
		}
		params.Tools = append(params.Tools, t)
	}
	if webSearchEnabled {
		params.Tools = append(params.Tools, claudeWebSearchTool)
	}
	adapter := &ClaudeAdapter{
		provider: provider,
		params:   params,
	}
	if mcpConfig == nil || len(mcpConfig.Servers) == 0 {
		return adapter
	}

	betaParams, err := buildClaudeBetaMCPParams(params, mcpConfig)
	if err != nil {
		adapter.initErr = fmt.Errorf("build claude beta MCP params: %w", err)
		return adapter
	}

	adapter.useBetaMCP = true
	adapter.betaParams = betaParams
	return adapter
}

// Call makes one Messages API request. It returns a final GenerateResponse when
// the model produces a text answer, or a non-empty []ToolUse when tools are
// requested (GenerateResponse is nil in that case).
func (a *ClaudeAdapter) Call(ctx context.Context) (*GenerateResponse, []ToolUse, error) {
	if a.initErr != nil {
		return nil, nil, a.initErr
	}
	if a.useBetaMCP {
		var (
			msg *anthropic.BetaMessage
			err error
		)
		if a.textDeltaHandler != nil {
			msg, err = a.provider.CallBetaWithRetryStreaming(ctx, a.betaParams, a.textDeltaHandler)
		} else {
			msg, err = a.provider.CallBeta(ctx, a.betaParams)
		}
		if err != nil {
			return nil, nil, WrapSafetyViolationError(models.SafetyViolationProviderAnthropic, fmt.Errorf("Anthropic Beta API call failed: %w", err))
		}
		a.webSearchCompleted += countWebSearchToolResultsInBetaMessage(msg)
		a.rawBetaMessages = append(a.rawBetaMessages, msg)
		toolUses := extractClaudeBetaToolUses(msg)
		if len(toolUses) == 0 {
			return a.provider.BetaToGenerateResponse(msg), nil, nil
		}
		appendClaudeBetaAssistantLoopTurn(&a.betaParams, msg, toolUses)
		return nil, toolUses, nil
	}

	var (
		msg *anthropic.Message
		err error
	)
	if a.textDeltaHandler != nil {
		msg, err = a.provider.CallWithRetryStreaming(ctx, a.params, a.textDeltaHandler)
	} else {
		msg, err = a.provider.Call(ctx, a.params)
	}
	if err != nil {
		return nil, nil, WrapSafetyViolationError(models.SafetyViolationProviderAnthropic, fmt.Errorf("Anthropic API call failed: %w", err))
	}
	a.webSearchCompleted += countWebSearchToolResultsInMessage(msg)
	a.rawMessages = append(a.rawMessages, msg)

	toolUses := extractClaudeToolUses(msg)
	if len(toolUses) == 0 {
		return a.provider.ToGenerateResponse(msg), nil, nil
	}

	// Persist assistant loop context for the next Call: replay native web search blocks
	// (with encrypted_content) via per-block ToParam(), not full msg.ToParam().
	appendClaudeAssistantLoopTurn(&a.params, msg, toolUses)

	return nil, toolUses, nil
}

// AppendToolResults converts results to Anthropic tool-result content blocks and
// appends them as a single user turn so the next Call has full context. Tool-produced
// images are appended in a following user turn (Anthropic forbids image blocks on
// assistant turns — same split as HistoryAssistantImageCaption for history).
func (a *ClaudeAdapter) AppendToolResults(results []ToolResult) {
	if a.useBetaMCP {
		var blocks []anthropic.BetaContentBlockParamUnion
		for _, r := range results {
			blocks = append(blocks, anthropic.NewBetaToolResultBlock(r.ID, toolResultOutput(r), r.IsErr))
		}
		a.betaParams.Messages = append(a.betaParams.Messages, anthropic.NewBetaUserMessage(blocks...))
		appendClaudeBetaToolResultImages(&a.betaParams, results)
		return
	}

	var blocks []anthropic.ContentBlockParamUnion
	for _, r := range results {
		blocks = append(blocks, anthropic.NewToolResultBlock(r.ID, toolResultOutput(r), r.IsErr))
	}
	a.params.Messages = append(a.params.Messages, anthropic.NewUserMessage(blocks...))
	appendClaudeToolResultImages(&a.params, results)
}

func (a *ClaudeAdapter) SetTextDeltaHandler(handler func(delta string)) {
	a.textDeltaHandler = handler
}

// ForceFinalResponse strips tools, appends a nudge, and issues one last Call.
func (a *ClaudeAdapter) ForceFinalResponse(ctx context.Context) (*GenerateResponse, error) {
	if a.useBetaMCP {
		a.betaParams.Tools = nil
		a.betaParams.Messages = append(a.betaParams.Messages, anthropic.NewBetaUserMessage(
			anthropic.NewBetaTextBlock("Please provide your best final response based on the information gathered so far without additional tool calls."),
		))
		var (
			msg *anthropic.BetaMessage
			err error
		)
		if a.textDeltaHandler != nil {
			msg, err = a.provider.CallBetaWithRetryStreaming(ctx, a.betaParams, a.textDeltaHandler)
		} else {
			msg, err = a.provider.CallBeta(ctx, a.betaParams)
		}
		if err != nil {
			return nil, WrapSafetyViolationError(models.SafetyViolationProviderAnthropic, fmt.Errorf("Anthropic beta final-response call failed: %w", err))
		}
		a.webSearchCompleted += countWebSearchToolResultsInBetaMessage(msg)
		a.rawBetaMessages = append(a.rawBetaMessages, msg)
		return a.provider.BetaToGenerateResponse(msg), nil
	}

	a.params.Tools = nil
	a.params.Messages = append(a.params.Messages, anthropic.NewUserMessage(
		anthropic.NewTextBlock("Please provide your best final response based on the information gathered so far without additional tool calls."),
	))

	var (
		msg *anthropic.Message
		err error
	)
	if a.textDeltaHandler != nil {
		msg, err = a.provider.CallWithRetryStreaming(ctx, a.params, a.textDeltaHandler)
	} else {
		msg, err = a.provider.Call(ctx, a.params)
	}
	if err != nil {
		return nil, WrapSafetyViolationError(models.SafetyViolationProviderAnthropic, fmt.Errorf("Anthropic final-response call failed: %w", err))
	}
	a.webSearchCompleted += countWebSearchToolResultsInMessage(msg)
	a.rawMessages = append(a.rawMessages, msg)
	return a.provider.ToGenerateResponse(msg), nil
}

// AllRawMessages returns Anthropic message payloads observed this turn.
func (a *ClaudeAdapter) AllRawMessages() []*anthropic.Message {
	if len(a.rawMessages) == 0 {
		return nil
	}
	out := make([]*anthropic.Message, len(a.rawMessages))
	copy(out, a.rawMessages)
	return out
}

// AllRawBetaMessages returns Anthropic beta message payloads observed this turn.
func (a *ClaudeAdapter) AllRawBetaMessages() []*anthropic.BetaMessage {
	if len(a.rawBetaMessages) == 0 {
		return nil
	}
	out := make([]*anthropic.BetaMessage, len(a.rawBetaMessages))
	copy(out, a.rawBetaMessages)
	return out
}

// WebSearchCompletedCount returns how many web search result blocks were observed
// across all adapter Call and ForceFinalResponse invocations this turn.
func (a *ClaudeAdapter) WebSearchCompletedCount() int {
	return a.webSearchCompleted
}

func countWebSearchToolResultsInMessage(msg *anthropic.Message) int {
	if msg == nil {
		return 0
	}
	n := 0
	for _, block := range msg.Content {
		if _, ok := block.AsAny().(anthropic.WebSearchToolResultBlock); ok {
			n++
		}
	}
	return n
}

func countWebSearchToolResultsInBetaMessage(msg *anthropic.BetaMessage) int {
	if msg == nil {
		return 0
	}
	n := 0
	for _, block := range msg.Content {
		if _, ok := block.AsAny().(anthropic.BetaWebSearchToolResultBlock); ok {
			n++
		}
	}
	return n
}

// extractClaudeToolUses returns all tool-use blocks from a Message normalised
// to the provider-agnostic ToolUse type.
func extractClaudeToolUses(msg *anthropic.Message) []ToolUse {
	if msg == nil {
		return nil
	}
	var uses []ToolUse
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
			uses = append(uses, ToolUse{
				ID:    tb.ID,
				Name:  tb.Name,
				Input: tb.Input,
			})
		}
	}
	return uses
}

func extractClaudeBetaToolUses(msg *anthropic.BetaMessage) []ToolUse {
	if msg == nil {
		return nil
	}
	var uses []ToolUse
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.BetaToolUseBlock); ok {
			rawInput, err := json.Marshal(tb.Input)
			if err != nil {
				rawInput = []byte("{}")
			}
			uses = append(uses, ToolUse{
				ID:    tb.ID,
				Name:  tb.Name,
				Input: rawInput,
			})
		}
	}
	return uses
}

func appendClaudeBetaAssistantLoopTurn(params *anthropic.BetaMessageNewParams, msg *anthropic.BetaMessage, uses []ToolUse) {
	if params == nil || msg == nil || len(uses) == 0 {
		return
	}
	useIDs := make(map[string]struct{}, len(uses))
	for _, u := range uses {
		useIDs[u.ID] = struct{}{}
	}

	var blocks []anthropic.BetaContentBlockParamUnion
	var webSearchTextFallback []string

	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.BetaTextBlock:
			if strings.TrimSpace(v.Text) != "" {
				// See the non-beta path below: intermediate native web-search
				// citations may contain an empty URL and cannot be resubmitted.
				blocks = append(blocks, anthropic.NewBetaTextBlock(v.Text))
			}
		case anthropic.BetaServerToolUseBlock:
			blocks = append(blocks, block.ToParam())
		case anthropic.BetaWebSearchToolResultBlock:
			if claudeBetaWebSearchToolResultReplayable(v) {
				blocks = append(blocks, block.ToParam())
			} else if out := FormatClaudeBetaWebSearchResultBlock(v); out != "" {
				webSearchTextFallback = append(webSearchTextFallback, out)
			}
		case anthropic.BetaToolUseBlock:
			if _, ok := useIDs[v.ID]; ok {
				blocks = append(blocks, block.ToParam())
			}
		}
	}

	if len(blocks) == 0 {
		return
	}
	params.Messages = append(params.Messages, anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRoleAssistant,
		Content: blocks,
	})
	if len(webSearchTextFallback) > 0 {
		appendClaudeBetaInLoopWebSearchContextText(params, ClaudeInLoopWebSearchContextHeader+strings.Join(webSearchTextFallback, "\n\n"))
	}
}

// appendClaudeAssistantLoopTurn appends the assistant turn for the in-turn agent loop.
// Client tool_use, server_tool_use, and web_search_tool_result blocks are replayed via
// per-block ToParam() (preserving encrypted_content for Anthropic server-side decryption).
// Full Message.ToParam() is avoided — it can mangle web search content on resubmit.
// Non-replayable web search payloads fall back to formatted user text.
func appendClaudeAssistantLoopTurn(params *anthropic.MessageNewParams, msg *anthropic.Message, uses []ToolUse) {
	if params == nil || msg == nil || len(uses) == 0 {
		return
	}
	useIDs := make(map[string]struct{}, len(uses))
	for _, u := range uses {
		useIDs[u.ID] = struct{}{}
	}

	var blocks []anthropic.ContentBlockParamUnion
	var webSearchTextFallback []string

	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			if strings.TrimSpace(v.Text) != "" {
				// Citations on intermediate native web-search text are owned by
				// Anthropic. Some responses contain an empty location URL, which
				// their API rejects when this turn is replayed. The text itself is
				// useful, while the encrypted web-search result below preserves
				// native search continuity, so deliberately omit citations here.
				blocks = append(blocks, anthropic.NewTextBlock(v.Text))
			}
		case anthropic.ServerToolUseBlock:
			blocks = append(blocks, block.ToParam())
		case anthropic.WebSearchToolResultBlock:
			if claudeWebSearchToolResultReplayable(v) {
				blocks = append(blocks, block.ToParam())
			} else if out := FormatClaudeWebSearchResultBlock(v); out != "" {
				webSearchTextFallback = append(webSearchTextFallback, out)
			}
		case anthropic.ToolUseBlock:
			if _, ok := useIDs[v.ID]; ok {
				blocks = append(blocks, block.ToParam())
			}
		}
	}

	if len(blocks) == 0 {
		return
	}
	params.Messages = append(params.Messages, anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleAssistant,
		Content: blocks,
	})
	if len(webSearchTextFallback) > 0 {
		appendClaudeInLoopWebSearchContextText(params, ClaudeInLoopWebSearchContextHeader+strings.Join(webSearchTextFallback, "\n\n"))
	}
}

func claudeWebSearchToolResultReplayable(ws anthropic.WebSearchToolResultBlock) bool {
	if len(claudeWebSearchResultsFromContent(ws.Content)) > 0 {
		return true
	}
	err := ws.Content.AsResponseWebSearchToolResultError()
	return err.ErrorCode != ""
}

func claudeBetaWebSearchToolResultReplayable(ws anthropic.BetaWebSearchToolResultBlock) bool {
	if len(ws.Content.AsBetaWebSearchResultBlockArray()) > 0 {
		return true
	}
	err := ws.Content.AsResponseWebSearchToolResultError()
	return err.ErrorCode != ""
}

func appendClaudeToolResultImages(params *anthropic.MessageNewParams, results []ToolResult) {
	if params == nil {
		return
	}
	for _, r := range results {
		blocks := claudeImageBlocksFromUserImages(r.Images, GeneratedToolImageCaption)
		if len(blocks) == 0 {
			continue
		}
		params.Messages = append(params.Messages, anthropic.NewUserMessage(blocks...))
	}
}

func appendClaudeBetaToolResultImages(params *anthropic.BetaMessageNewParams, results []ToolResult) {
	if params == nil {
		return
	}
	for _, r := range results {
		blocks := claudeBetaImageBlocksFromUserImages(r.Images, GeneratedToolImageCaption)
		if len(blocks) == 0 {
			continue
		}
		params.Messages = append(params.Messages, anthropic.NewBetaUserMessage(blocks...))
	}
}

func claudeBetaImageBlocksFromUserImages(images []UserMessageImage, leadingText string) []anthropic.BetaContentBlockParamUnion {
	var imageBlocks []anthropic.BetaContentBlockParamUnion
	for _, im := range images {
		if len(im.RawBytes) == 0 {
			continue
		}
		mt := im.MediaType
		if strings.TrimSpace(mt) == "" {
			mt = "image/jpeg"
		}
		imageBlocks = append(imageBlocks, anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
			Data:      base64.StdEncoding.EncodeToString(im.RawBytes),
			MediaType: anthropic.BetaBase64ImageSourceMediaType(mt),
		}))
	}
	if len(imageBlocks) == 0 {
		return nil
	}
	blocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(imageBlocks)+1)
	if t := strings.TrimSpace(leadingText); t != "" {
		blocks = append(blocks, anthropic.NewBetaTextBlock(t))
	}
	return append(blocks, imageBlocks...)
}

func appendClaudeInLoopWebSearchContextText(params *anthropic.MessageNewParams, text string) {
	if params == nil || strings.TrimSpace(text) == "" {
		return
	}
	params.Messages = append(params.Messages, anthropic.NewUserMessage(
		anthropic.NewTextBlock(text),
	))
}

func appendClaudeBetaInLoopWebSearchContextText(params *anthropic.BetaMessageNewParams, text string) {
	if params == nil || strings.TrimSpace(text) == "" {
		return
	}
	params.Messages = append(params.Messages, anthropic.NewBetaUserMessage(
		anthropic.NewBetaTextBlock(text),
	))
}

// BuildClaudeBetaMCPParams converts standard MessageNewParams + an MCP config into
// BetaMessageNewParams. Exported so subagent tooling can use it without going through the adapter.
func BuildClaudeBetaMCPParams(params anthropic.MessageNewParams, mcpConfig *ClaudeMCPConfig) (anthropic.BetaMessageNewParams, error) {
	return buildClaudeBetaMCPParams(params, mcpConfig)
}

func buildClaudeBetaMCPParams(params anthropic.MessageNewParams, mcpConfig *ClaudeMCPConfig) (anthropic.BetaMessageNewParams, error) {
	out := anthropic.BetaMessageNewParams{
		MaxTokens:     params.MaxTokens,
		Model:         params.Model,
		InferenceGeo:  params.InferenceGeo,
		Temperature:   params.Temperature,
		TopK:          params.TopK,
		TopP:          params.TopP,
		StopSequences: append([]string(nil), params.StopSequences...),
	}

	if err := copyViaJSON("messages", params.Messages, &out.Messages); err != nil {
		return out, err
	}
	if err := copyViaJSON("system", params.System, &out.System); err != nil {
		return out, err
	}
	betaTools, err := convertClaudeToolUnionsToBeta(params.Tools)
	if err != nil {
		return out, err
	}
	out.Tools = betaTools
	if err := copyViaJSON("container", params.Container, &out.Container); err != nil {
		return out, err
	}
	if err := copyViaJSON("cache_control", params.CacheControl, &out.CacheControl); err != nil {
		return out, err
	}
	if err := copyViaJSON("metadata", params.Metadata, &out.Metadata); err != nil {
		return out, err
	}
	if err := copyViaJSON("output_config", params.OutputConfig, &out.OutputConfig); err != nil {
		return out, err
	}
	if err := copyViaJSON("service_tier", params.ServiceTier, &out.ServiceTier); err != nil {
		return out, err
	}
	if err := copyViaJSONIfPresent("thinking", params.Thinking, &out.Thinking); err != nil {
		return out, err
	}
	if err := copyViaJSONIfPresent("tool_choice", params.ToolChoice, &out.ToolChoice); err != nil {
		return out, err
	}

	out.Betas = append(out.Betas, anthropic.AnthropicBetaMCPClient2025_11_20)
	out.MCPServers = append(out.MCPServers, mcpConfig.Servers...)
	out.Tools = append(out.Tools, mcpConfig.Toolsets...)
	// copyViaJSON uses encoding/json; ToolInputSchemaParam stores additionalProperties in
	// ExtraFields (json:"-"), which does not round-trip into BetaToolInputSchemaParam. The
	// beta Messages API requires additionalProperties: false on every object in input_schema.
	patchBetaToolsInputSchemaAdditionalProperties(out.Tools)
	return out, nil
}

// convertClaudeToolUnionsToBeta converts tools without json.Marshal/Unmarshal on the whole slice.
// A full JSON round-trip stashes unknown keys on SDK paramObj metadata and nested structs; the beta
// API then rejects payloads (e.g. input_schema on web_search, or extra fields under user_location).
func convertClaudeToolUnionsToBeta(in []anthropic.ToolUnionParam) ([]anthropic.BetaToolUnionParam, error) {
	out := make([]anthropic.BetaToolUnionParam, 0, len(in))
	for i := range in {
		b, err := convertOneToolUnionToBeta(in[i])
		if err != nil {
			return nil, fmt.Errorf("tools[%d]: %w", i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

func convertOneToolUnionToBeta(u anthropic.ToolUnionParam) (anthropic.BetaToolUnionParam, error) {
	if u.OfTool != nil {
		bp, err := toolParamToBeta(u.OfTool)
		if err != nil {
			return anthropic.BetaToolUnionParam{}, err
		}
		return anthropic.BetaToolUnionParam{OfTool: &bp}, nil
	}
	if u.OfWebSearchTool20250305 != nil {
		return webSearchTool20250305ToBeta(u.OfWebSearchTool20250305), nil
	}
	return anthropic.BetaToolUnionParam{}, fmt.Errorf("unsupported ToolUnionParam variant for beta MCP (add explicit conversion)")
}

func toolParamToBeta(tp *anthropic.ToolParam) (anthropic.BetaToolParam, error) {
	var betaSchema anthropic.BetaToolInputSchemaParam
	if err := copyViaJSON("input_schema", tp.InputSchema, &betaSchema); err != nil {
		return anthropic.BetaToolParam{}, err
	}
	patchBetaToolInputSchema(&betaSchema)
	return anthropic.BetaToolParam{
		InputSchema:         betaSchema,
		Name:                tp.Name,
		EagerInputStreaming: tp.EagerInputStreaming,
		DeferLoading:        tp.DeferLoading,
		Description:         tp.Description,
		Strict:              tp.Strict,
		Type:                anthropic.BetaToolType(tp.Type),
		AllowedCallers:      append([]string(nil), tp.AllowedCallers...),
		CacheControl:        cacheControlEphemeralToBeta(tp.CacheControl),
		InputExamples:       slices.Clone(tp.InputExamples),
	}, nil
}

func webSearchTool20250305ToBeta(ws *anthropic.WebSearchTool20250305Param) anthropic.BetaToolUnionParam {
	return anthropic.BetaToolUnionParam{
		OfWebSearchTool20250305: &anthropic.BetaWebSearchTool20250305Param{
			MaxUses:        ws.MaxUses,
			DeferLoading:   ws.DeferLoading,
			Strict:         ws.Strict,
			AllowedDomains: append([]string(nil), ws.AllowedDomains...),
			BlockedDomains: append([]string(nil), ws.BlockedDomains...),
			AllowedCallers: append([]string(nil), ws.AllowedCallers...),
			CacheControl:   cacheControlEphemeralToBeta(ws.CacheControl),
			UserLocation:   userLocationToBeta(ws.UserLocation),
			Name:           ws.Name,
			Type:           ws.Type,
		},
	}
}

func cacheControlEphemeralToBeta(c anthropic.CacheControlEphemeralParam) anthropic.BetaCacheControlEphemeralParam {
	return anthropic.BetaCacheControlEphemeralParam{
		TTL:  anthropic.BetaCacheControlEphemeralTTL(c.TTL),
		Type: c.Type,
	}
}

func userLocationToBeta(u anthropic.UserLocationParam) anthropic.BetaUserLocationParam {
	return anthropic.BetaUserLocationParam{
		City:     u.City,
		Country:  u.Country,
		Region:   u.Region,
		Timezone: u.Timezone,
		Type:     u.Type,
	}
}

func patchBetaToolsInputSchemaAdditionalProperties(tools []anthropic.BetaToolUnionParam) {
	for i := range tools {
		t := tools[i].OfTool
		if t == nil {
			continue
		}
		patchBetaToolInputSchema(&t.InputSchema)
	}
}

func patchBetaToolInputSchema(schema *anthropic.BetaToolInputSchemaParam) {
	if schema == nil {
		return
	}
	if schema.ExtraFields == nil {
		schema.ExtraFields = make(map[string]any)
	}
	schema.ExtraFields["additionalProperties"] = false
	if schema.Properties != nil {
		schema.Properties = ensureObjectSchemaAdditionalProperties(schema.Properties)
	}
}

// ensureObjectSchemaAdditionalProperties walks a JSON-schema-shaped value and sets
// additionalProperties: false on every object schema (type "object"), including nested
// properties/items/anyOf/oneOf, matching Anthropic beta tool validation.
func ensureObjectSchemaAdditionalProperties(v any) any {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, inner := range val {
			out[k] = ensureObjectSchemaAdditionalProperties(inner)
		}
		if typ, ok := out["type"].(string); ok && typ == "object" {
			out["additionalProperties"] = false
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, inner := range val {
			out[i] = ensureObjectSchemaAdditionalProperties(inner)
		}
		return out
	default:
		return v
	}
}

func copyViaJSON[S any, D any](field string, src S, dst *D) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", field, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("unmarshal %s: %w", field, err)
	}
	return nil
}

func copyViaJSONIfPresent[S any, D any](field string, src S, dst *D) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", field, err)
	}
	// Anthropic union params marshal to {} when unset; skip in that case.
	if string(raw) == "{}" || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("unmarshal %s: %w", field, err)
	}
	return nil
}
