package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const (
	defaultDiscoveryTTL = 5 * time.Minute
	streamableAccept    = "application/json, text/event-stream"
)

var toolNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_]`)

type ConnectorTool struct {
	ConnectorID     uuid.UUID
	ConnectorName   string
	ConnectorStatus string
	Name            string
	FullName        string
	Description     string
	Properties      map[string]any
	Required        []string
}

type DiscoveryResult struct {
	Tools  []ConnectorTool
	Errors map[uuid.UUID]string
}

type Client struct {
	httpClient *http.Client
	logger     *zap.Logger
	ttl        time.Duration

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	tools   []ConnectorTool
	expires time.Time
}

func New(httpClient *http.Client, logger *zap.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Client{
		httpClient: httpClient,
		logger:     logger,
		ttl:        defaultDiscoveryTTL,
		cache:      make(map[string]cacheEntry),
	}
}

func (c *Client) Invalidate(connectorID uuid.UUID) {
	c.mu.Lock()
	delete(c.cache, connectorID.String())
	c.mu.Unlock()
}

// ProbeConnection runs initialize + tools/list against a connector configuration
// without mutating cache or persistence state and returns discovered tool count.
func (c *Client) ProbeConnection(ctx context.Context, server *models.MCPServer) (int, error) {
	if server == nil {
		return 0, fmt.Errorf("server is required")
	}
	tools, err := c.listToolsRPC(ctx, server)
	if err != nil {
		return 0, err
	}
	return len(tools), nil
}

func (c *Client) DiscoverTools(ctx context.Context, userID uuid.UUID, servers []*models.MCPServer) DiscoveryResult {
	res := DiscoveryResult{
		Tools:  make([]ConnectorTool, 0),
		Errors: make(map[uuid.UUID]string),
	}
	for _, s := range servers {
		if s == nil || s.ID == uuid.Nil {
			continue
		}
		if !connectorEligible(s) {
			res.Errors[s.ID] = fmt.Sprintf("connector status %q not eligible", strings.TrimSpace(s.Status))
			continue
		}
		tools, err := c.discoverConnectorTools(ctx, userID, s)
		if err != nil {
			res.Errors[s.ID] = err.Error()
			continue
		}
		res.Tools = append(res.Tools, tools...)
	}
	return res
}

func connectorEligible(s *models.MCPServer) bool {
	status := strings.TrimSpace(s.Status)
	if status == "" {
		status = models.MCPServerStatusActive
	}
	switch status {
	case models.MCPServerStatusActive, models.MCPServerStatusRefreshError:
		return true
	default:
		return false
	}
}

func (c *Client) discoverConnectorTools(ctx context.Context, userID uuid.UUID, server *models.MCPServer) ([]ConnectorTool, error) {
	cacheKey := server.ID.String()
	now := time.Now().UTC()
	c.mu.RLock()
	if e, ok := c.cache[cacheKey]; ok && now.Before(e.expires) {
		out := make([]ConnectorTool, len(e.tools))
		copy(out, e.tools)
		c.mu.RUnlock()
		return out, nil
	}
	c.mu.RUnlock()

	rawTools, err := c.listToolsRPC(ctx, server)
	if err != nil {
		return nil, err
	}

	prefix := connectorPrefix(server)
	out := make([]ConnectorTool, 0, len(rawTools))
	for _, t := range rawTools {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		fullName := "mcp__" + prefix + "__" + sanitizeForToolName(name)
		props, req := parseInputSchema(t.InputSchema)
		out = append(out, ConnectorTool{
			ConnectorID:     server.ID,
			ConnectorName:   server.Name,
			ConnectorStatus: strings.TrimSpace(server.Status),
			Name:            name,
			FullName:        fullName,
			Description:     mcpToolDescription(server, t),
			Properties:      props,
			Required:        req,
		})
	}

	c.mu.Lock()
	c.cache[cacheKey] = cacheEntry{
		tools:   out,
		expires: now.Add(c.ttl),
	}
	c.mu.Unlock()

	return out, nil
}

func (c *Client) CallToolByFullName(ctx context.Context, servers []*models.MCPServer, fullName string, rawArgs json.RawMessage) (string, error) {
	connectorKey, toolName, err := parseFullToolName(fullName)
	if err != nil {
		return "", err
	}
	server := findServerByPrefix(servers, connectorKey)
	if server == nil {
		return "", fmt.Errorf("unknown mcp connector prefix %q", connectorKey)
	}
	if !connectorEligible(server) {
		return "", fmt.Errorf("connector %q is not available in status %q", server.Name, server.Status)
	}
	tools, err := c.discoverConnectorTools(ctx, uuid.Nil, server)
	if err != nil {
		return "", err
	}
	original := toolName
	for _, t := range tools {
		if t.FullName == fullName {
			original = t.Name
			break
		}
	}
	return c.callToolRPC(ctx, server, original, rawArgs)
}

func findServerByPrefix(servers []*models.MCPServer, prefix string) *models.MCPServer {
	for _, s := range servers {
		if s == nil {
			continue
		}
		if connectorPrefix(s) == prefix {
			return s
		}
	}
	return nil
}

func connectorPrefix(server *models.MCPServer) string {
	if server == nil {
		return ""
	}
	if server.ID != uuid.Nil {
		id := strings.ReplaceAll(server.ID.String(), "-", "")
		if len(id) >= 8 {
			return id[:8]
		}
		return id
	}
	return sanitizeForToolName(server.Name)
}

func sanitizeForToolName(name string) string {
	s := strings.TrimSpace(strings.ToLower(name))
	s = strings.ReplaceAll(s, " ", "_")
	s = toolNameSanitizer.ReplaceAllString(s, "_")
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	s = strings.Trim(s, "_")
	if s == "" {
		return "tool"
	}
	return s
}

func parseFullToolName(name string) (connectorPrefix string, toolName string, err error) {
	parts := strings.Split(name, "__")
	if len(parts) < 3 || parts[0] != "mcp" {
		return "", "", fmt.Errorf("invalid mcp tool name %q", name)
	}
	connectorPrefix = parts[1]
	toolName = strings.Join(parts[2:], "__")
	if connectorPrefix == "" || toolName == "" {
		return "", "", fmt.Errorf("invalid mcp tool name %q", name)
	}
	return connectorPrefix, toolName, nil
}

func mcpToolDescription(server *models.MCPServer, tool mcpToolDefinition) string {
	base := strings.TrimSpace(tool.Description)
	if base == "" {
		base = fmt.Sprintf("Tool %q from connector %q.", tool.Name, server.Name)
	}
	return base + fmt.Sprintf(" (MCP connector: %s)", strings.TrimSpace(server.Name))
}

func parseInputSchema(schema map[string]any) (map[string]any, []string) {
	if schema == nil {
		return map[string]any{}, nil
	}
	props := map[string]any{}
	if raw, ok := schema["properties"].(map[string]any); ok {
		props = raw
	}
	var required []string
	if rawReq, ok := schema["required"].([]any); ok {
		for _, v := range rawReq {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				required = append(required, s)
			}
		}
	}
	if rawReq, ok := schema["required"].([]string); ok {
		required = append(required, rawReq...)
	}
	if len(props) == 0 {
		props = map[string]any{
			"type": map[string]any{
				"type": "object",
			},
		}
	}
	return props, required
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func (c *Client) listToolsRPC(ctx context.Context, server *models.MCPServer) ([]mcpToolDefinition, error) {
	if err := c.initializeRPC(ctx, server); err != nil {
		c.logger.Debug("mcp initialize failed before tools/list; trying tools/list anyway", zap.String("server_id", server.ID.String()), zap.Error(err))
	}
	result, err := c.rpcCall(ctx, server, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []mcpToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, fmt.Errorf("decode tools/list response: %w", err)
	}
	return parsed.Tools, nil
}

func (c *Client) initializeRPC(ctx context.Context, server *models.MCPServer) error {
	_, err := c.rpcCall(ctx, server, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo": map[string]any{
			"name":    "what-iff",
			"version": "1.0.0",
		},
		"capabilities": map[string]any{},
	})
	if err != nil {
		return err
	}
	// Best-effort notification: some servers expect this, others ignore it.
	_ = c.rpcNotify(ctx, server, "notifications/initialized", map[string]any{})
	return nil
}

func (c *Client) callToolRPC(ctx context.Context, server *models.MCPServer, toolName string, rawArgs json.RawMessage) (string, error) {
	args := map[string]any{}
	if len(bytes.TrimSpace(rawArgs)) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("decode tool input: %w", err)
		}
	}
	result, err := c.rpcCall(ctx, server, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}
	var parsed struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(result, &parsed); err == nil && len(parsed.Content) > 0 {
		var textParts []string
		for _, part := range parsed.Content {
			if text, ok := part["text"].(string); ok && strings.TrimSpace(text) != "" {
				textParts = append(textParts, text)
			}
		}
		if len(textParts) > 0 {
			return strings.Join(textParts, "\n"), nil
		}
	}
	return string(result), nil
}

func (c *Client) rpcNotify(ctx context.Context, server *models.MCPServer, method string, params any) error {
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(server.ServerURL), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", streamableAccept)
	if token := strings.TrimSpace(server.AuthToken); token != "" {
		httpReq.Header.Set("Authorization", token)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) rpcCall(ctx context.Context, server *models.MCPServer, method string, params any) (json.RawMessage, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      uuid.NewString(),
		Method:  method,
		Params:  params,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(server.ServerURL), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", streamableAccept)
	if token := strings.TrimSpace(server.AuthToken); token != "" {
		httpReq.Header.Set("Authorization", token)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		detail := strings.TrimSpace(string(body))
		if detail != "" {
			return nil, fmt.Errorf("mcp server returned status %d for %s: %s", resp.StatusCode, method, truncateForError(detail, 1024))
		}
		return nil, fmt.Errorf("mcp server returned status %d for %s", resp.StatusCode, method)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	rpcResp, err := parseRPCResponse(body, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp rpc %s failed: %s (%d)", method, rpcResp.Error.Message, rpcResp.Error.Code)
	}
	return rpcResp.Result, nil
}

func parseRPCResponse(body []byte, contentType string) (rpcResponse, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return rpcResponse{}, fmt.Errorf("empty mcp rpc response body")
	}

	var out rpcResponse
	if err := json.Unmarshal(trimmed, &out); err == nil {
		return out, nil
	}

	lowerContentType := strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(lowerContentType, "text/event-stream") || bytes.HasPrefix(trimmed, []byte("event:")) || bytes.Contains(trimmed, []byte("\ndata:")) {
		payload, err := extractSSEJSONPayload(trimmed)
		if err != nil {
			return rpcResponse{}, err
		}
		if err := json.Unmarshal(payload, &out); err != nil {
			return rpcResponse{}, fmt.Errorf("decode mcp sse json response: %w", err)
		}
		return out, nil
	}

	return rpcResponse{}, fmt.Errorf("decode mcp json response: invalid JSON payload")
}

func extractSSEJSONPayload(body []byte) ([]byte, error) {
	lines := strings.Split(string(body), "\n")
	var currentData []string
	var lastData string

	flush := func() ([]byte, bool) {
		if len(currentData) == 0 {
			return nil, false
		}
		candidate := strings.TrimSpace(strings.Join(currentData, "\n"))
		currentData = currentData[:0]
		if candidate == "" || candidate == "[DONE]" {
			return nil, false
		}
		lastData = candidate
		if json.Valid([]byte(candidate)) {
			return []byte(candidate), true
		}
		return nil, false
	}

	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			if payload, ok := flush(); ok {
				return payload, nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			currentData = append(currentData, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if payload, ok := flush(); ok {
		return payload, nil
	}
	if lastData != "" {
		return nil, fmt.Errorf("mcp sse data was not valid json: %s", truncateForError(lastData, 512))
	}
	return nil, fmt.Errorf("mcp sse response did not contain a data payload")
}

func truncateForError(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
