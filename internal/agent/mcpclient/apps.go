package mcpclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// MCP Apps (SEP-1865, extension io.modelcontextprotocol/ui; ADR 0x042): a tool may name a ui://
// HTML resource in its _meta. After the tool runs, the chat renders that resource in a sandboxed
// frame and sends it the tool's input and result; the view can call the server's tools back
// through the host. A tool's visibility says who may call it: the model, the view, or both.

const (
	// maxAppResourceBytes bounds an MCP App's HTML.
	maxAppResourceBytes = 2 << 20
	// MaxAppResultBytes bounds the raw tool result kept for a view.
	MaxAppResultBytes = 256 << 10
)

// toolUIMeta is a tool's _meta.ui: its view and who may call it.
type toolUIMeta struct {
	ResourceURI string
	// Visibility lists "model" and/or "app"; empty means both.
	Visibility []string
}

func (m toolUIMeta) visibleTo(who string) bool {
	return len(m.Visibility) == 0 || slices.Contains(m.Visibility, who)
}

// parseToolUIMeta reads a tool's _meta: the current nested form (_meta.ui.resourceUri,
// _meta.ui.visibility) and the deprecated flat key (_meta["ui/resourceUri"]).
func parseToolUIMeta(meta map[string]any) toolUIMeta {
	var out toolUIMeta
	if meta == nil {
		return out
	}
	if ui, ok := meta["ui"].(map[string]any); ok {
		if uri, ok := ui["resourceUri"].(string); ok {
			out.ResourceURI = strings.TrimSpace(uri)
		}
		if vis, ok := ui["visibility"].([]any); ok {
			for _, v := range vis {
				if s, ok := v.(string); ok && (s == "model" || s == "app") {
					out.Visibility = append(out.Visibility, s)
				}
			}
			if len(out.Visibility) == 0 {
				// A list with nothing we know: treat it as visible to neither rather than both.
				out.Visibility = []string{"none"}
			}
		}
	}
	if out.ResourceURI == "" {
		if uri, ok := meta["ui/resourceUri"].(string); ok {
			out.ResourceURI = strings.TrimSpace(uri)
		}
	}
	if !strings.HasPrefix(out.ResourceURI, "ui://") {
		out.ResourceURI = ""
	}
	return out
}

// AppCSPDomains are the origins an MCP App declares it needs (resource _meta.ui.csp).
type AppCSPDomains struct {
	ConnectDomains  []string `json:"connectDomains,omitempty"`
	ResourceDomains []string `json:"resourceDomains,omitempty"`
	FrameDomains    []string `json:"frameDomains,omitempty"`
	BaseURIDomains  []string `json:"baseUriDomains,omitempty"`
}

// AppResource is an MCP App's view as the server serves it.
type AppResource struct {
	URI      string
	MIMEType string
	HTML     string
	CSP      *AppCSPDomains
	// PrefersBorder is the view's wish for a visible border (nil: no preference).
	PrefersBorder *bool
}

// ReadAppResource fetches an MCP App's HTML (resources/read) from server.
func (c *Client) ReadAppResource(ctx context.Context, server *models.MCPServer, uri string, sessions SessionState) (AppResource, error) {
	if !strings.HasPrefix(uri, "ui://") {
		return AppResource{}, fmt.Errorf("not an MCP App resource: %q", uri)
	}
	if sessions == nil {
		sessions = make(SessionState)
	}
	if err := c.initializeRPC(ctx, server, sessions); err != nil {
		return AppResource{}, err
	}
	raw, err := c.rpcCall(ctx, server, "resources/read", map[string]any{"uri": uri}, sessions)
	if err != nil {
		return AppResource{}, err
	}
	var parsed struct {
		Contents []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
			Text     string `json:"text"`
			Blob     string `json:"blob"`
			Meta     struct {
				UI struct {
					CSP           *AppCSPDomains `json:"csp"`
					PrefersBorder *bool          `json:"prefersBorder"`
				} `json:"ui"`
			} `json:"_meta"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return AppResource{}, fmt.Errorf("decode resources/read response: %w", err)
	}
	for _, content := range parsed.Contents {
		if content.URI != "" && content.URI != uri {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(content.MIMEType)), "text/html") {
			return AppResource{}, fmt.Errorf("MCP App resource %q is %q, not HTML", uri, content.MIMEType)
		}
		html := content.Text
		if html == "" && content.Blob != "" {
			decoded, err := base64.StdEncoding.DecodeString(content.Blob)
			if err != nil {
				return AppResource{}, fmt.Errorf("MCP App resource %q: invalid base64", uri)
			}
			html = string(decoded)
		}
		if strings.TrimSpace(html) == "" {
			return AppResource{}, fmt.Errorf("MCP App resource %q is empty", uri)
		}
		if len(html) > maxAppResourceBytes {
			return AppResource{}, fmt.Errorf("MCP App resource %q is larger than %d bytes", uri, maxAppResourceBytes)
		}
		return AppResource{
			URI:           uri,
			MIMEType:      content.MIMEType,
			HTML:          html,
			CSP:           content.Meta.UI.CSP,
			PrefersBorder: content.Meta.UI.PrefersBorder,
		}, nil
	}
	return AppResource{}, fmt.Errorf("server returned no content for %q", uri)
}

// ErrToolNotForApps refuses a view's call to a tool whose visibility leaves out "app".
var ErrToolNotForApps = fmt.Errorf("this tool cannot be called from an app")

// CallAppTool runs a tool on behalf of an MCP App's view and returns the raw CallToolResult. Only
// a tool the server lists as visible to apps may be called (the spec: hosts MUST reject others),
// and only on the view's own server (the caller passes that server).
func (c *Client) CallAppTool(ctx context.Context, server *models.MCPServer, toolName string, args json.RawMessage, sessions SessionState) (json.RawMessage, error) {
	if sessions == nil {
		sessions = make(SessionState)
	}
	meta, ok, err := c.toolMeta(ctx, server, toolName, sessions)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", toolName)
	}
	if !meta.visibleTo("app") {
		return nil, ErrToolNotForApps
	}
	if len(args) == 0 || !json.Valid(args) {
		args = json.RawMessage(`{}`)
	}
	return c.rpcCallTool(ctx, server, map[string]any{"name": toolName, "arguments": args}, sessions, toolName)
}

// toolMeta looks up a tool's UI metadata on server from the discovery cache (refreshing it).
func (c *Client) toolMeta(ctx context.Context, server *models.MCPServer, toolName string, sessions SessionState) (toolUIMeta, bool, error) {
	if _, err := c.discoverConnectorTools(ctx, server.UserID, server, sessions); err != nil {
		return toolUIMeta{}, false, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.cache[server.ID.String()]
	if !ok {
		return toolUIMeta{}, false, nil
	}
	meta, ok := e.meta[toolName]
	return meta, ok, nil
}

// appOnlyTool reports whether the cached discovery lists name as a tool only an MCP App's view may
// call, which the model must not reach even by guessing its name.
func (c *Client) appOnlyTool(server *models.MCPServer, name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.cache[server.ID.String()]
	if !ok {
		return false
	}
	meta, ok := e.meta[name]
	return ok && !meta.visibleTo("model")
}

// cspSourcePattern is an origin an app may declare: https or wss, a host (optionally a leading
// *. wildcard) and an optional port. Anything else (quotes, keywords, paths, semicolons) is
// dropped so a server cannot widen or break the policy.
var cspSourcePattern = regexp.MustCompile(`^(https|wss)://(\*\.)?[a-zA-Z0-9-]+(\.[a-zA-Z0-9-]+)*(:[0-9]{1,5})?$`)

func cspSources(domains []string) []string {
	var out []string
	for _, d := range domains {
		d = strings.TrimRight(strings.TrimSpace(d), "/")
		if cspSourcePattern.MatchString(d) && !slices.Contains(out, d) {
			out = append(out, d)
		}
		if len(out) >= 32 {
			break
		}
	}
	return out
}

// AppContentSecurityPolicy is the policy an MCP App's view runs under: inline script and style
// only, plus the origins it declared for each kind of fetch; with none declared, it can load
// nothing from the network. The view runs in an opaque-origin frame, so 'self' would mean nothing.
func AppContentSecurityPolicy(csp *AppCSPDomains) string {
	if csp == nil {
		csp = &AppCSPDomains{}
	}
	join := func(base string, domains []string, none bool) string {
		parts := []string{}
		if base != "" {
			parts = append(parts, base)
		}
		parts = append(parts, cspSources(domains)...)
		if len(parts) == 0 && none {
			return "'none'"
		}
		return strings.Join(parts, " ")
	}
	resources := csp.ResourceDomains
	directives := []string{
		"default-src 'none'",
		"script-src " + join("'unsafe-inline'", resources, true),
		"style-src " + join("'unsafe-inline'", resources, true),
		"img-src " + join("data: blob:", resources, true),
		"font-src " + join("data:", resources, true),
		"media-src " + join("data: blob:", resources, true),
		"connect-src " + join("", csp.ConnectDomains, true),
		"frame-src " + join("", csp.FrameDomains, true),
		"base-uri " + join("", csp.BaseURIDomains, true),
		"form-action 'none'",
		"object-src 'none'",
	}
	return strings.Join(directives, "; ")
}

var doctypePattern = regexp.MustCompile(`(?is)^\s*<!doctype[^>]*>`)

// WithCSPMeta puts the policy at the very start of the view's document (after its doctype, so the
// page keeps standards mode), ahead of anything the view's own HTML loads. The parser places a
// leading meta in the head, so it applies to the whole document.
func WithCSPMeta(html, policy string) string {
	meta := `<meta http-equiv="Content-Security-Policy" content="` + strings.ReplaceAll(policy, `"`, "") + `">`
	if loc := doctypePattern.FindStringIndex(html); loc != nil {
		return html[:loc[1]] + meta + html[loc[1]:]
	}
	return meta + html
}
