package providerconnections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// listTimeout bounds one listing, every page included. A server that has not
// answered by then is reported as unreachable.
const listTimeout = 20 * time.Second

// A listing stops at whichever of these it reaches first, keeping what it has.
// They are far above any real provider (OpenRouter lists a few hundred models)
// and keep a misbehaving server from being asked forever.
const (
	maxListPages  = 50
	maxListModels = 5000
)

// ListedModel is one model a connection's provider offers.
type ListedModel struct {
	ID string `json:"id"`
	// DisplayName is the provider's own name for the model, when it gives one.
	DisplayName string `json:"display_name,omitempty"`
	// ToolSupport and VisionSupport are what the provider reports about the
	// model, or nil where it reports nothing (ADR 0x027). OpenRouter reports
	// both; most OpenAI-compatible servers report neither.
	ToolSupport   *bool `json:"tool_support,omitempty"`
	VisionSupport *bool `json:"vision_support,omitempty"`
}

// reportedCapabilities is the part of an OpenAI-compatible model entry that
// some providers add beyond the id: OpenRouter's name, supported parameters and
// input modalities.
type reportedCapabilities struct {
	Name                string   `json:"name"`
	SupportedParameters []string `json:"supported_parameters"`
	Architecture        struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
}

// listedFromOpenAI reads one OpenAI-compatible model entry, with whatever
// capabilities the provider reports about it.
func listedFromOpenAI(id, raw string) ListedModel {
	m := ListedModel{ID: id}
	var extra reportedCapabilities
	if json.Unmarshal([]byte(raw), &extra) != nil {
		return m
	}
	if extra.Name != "" && extra.Name != id {
		m.DisplayName = extra.Name
	}
	if extra.SupportedParameters != nil {
		tools := slices.Contains(extra.SupportedParameters, "tools")
		m.ToolSupport = &tools
	}
	if extra.Architecture.InputModalities != nil {
		vision := slices.Contains(extra.Architecture.InputModalities, "image")
		m.VisionSupport = &vision
	}
	return m
}

// ConnectError says why a provider could not be listed, as a stable code
// (models.ErrCodeProvider*) the client can point at a field with.
type ConnectError struct {
	Code string
	Err  error
}

func (e *ConnectError) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *ConnectError) Unwrap() error { return e.Err }

// connectErrorFor turns a listing failure into the stable code for it.
//
// The SDKs' error types are read here rather than through ClassifyError's
// registry, which only knows them once internal/agent/provider has loaded.
func connectErrorFor(err error, badKeyStatus int) *ConnectError {
	kind := telemetry.ClassifyError(err)
	status := 0
	var oaiErr *openai.Error
	var antErr *anthropic.Error
	var checkErr *keyCheckError
	switch {
	case errors.As(err, &oaiErr):
		status = oaiErr.StatusCode
	case errors.As(err, &antErr):
		status = antErr.StatusCode
	case errors.As(err, &checkErr):
		status = checkErr.status
	}
	if status != 0 {
		kind = telemetry.ClassifyHTTPStatus(status)
		if status == badKeyStatus {
			kind = telemetry.ErrorTypeAuth
		}
	}
	code := models.ErrCodeProviderIncompatible
	switch kind {
	case telemetry.ErrorTypeNetwork, telemetry.ErrorTypeTimeout:
		code = models.ErrCodeProviderUnreachable
	case telemetry.ErrorTypeAuth:
		code = models.ErrCodeProviderKeyRejected
	case telemetry.ErrorTypeRateLimited, telemetry.ErrorTypeServer:
		code = models.ErrCodeProviderUnavailable
	}
	return &ConnectError{Code: code, Err: err}
}

// Lister reads the models a provider offers.
//
// Its HTTP client must not be the shared provider client: that one's
// credential transport replaces the key on any request to a built-in
// provider's host with the account's stored key, so a key being tested would
// never be the one sent. The SDK services are built without the SDKs' client
// defaults, which read keys from the process environment: a connection without
// a key sends none, never the deployment's.
type Lister struct {
	httpClient *http.Client
	// mock answers every listing with mockModels and sends nothing, for the
	// mock LLM backend.
	mock bool
}

// NewLister builds a Lister over httpClient. With mock set it never calls out.
func NewLister(httpClient *http.Client, mock bool) *Lister {
	return &Lister{httpClient: httpClient, mock: mock}
}

// mockModels is what every listing answers under the mock LLM backend.
var mockModels = []ListedModel{
	{ID: "mock-large", DisplayName: "Mock Large"},
	{ID: "mock-small", DisplayName: "Mock Small"},
}

// List returns the models the provider offers, sorted by id, checking the key
// where the target says how. The key may be empty for a server that needs
// none. A failure is a *ConnectError.
func (l *Lister) List(ctx context.Context, t Target) ([]ListedModel, error) {
	if l.mock {
		return append([]ListedModel(nil), mockModels...), nil
	}
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	var (
		out []ListedModel
		err error
	)
	switch t.Flavor {
	case FlavorOpenAI:
		out, err = l.listOpenAI(ctx, t.BaseURL, t.Key)
	case FlavorAnthropic:
		out, err = l.listAnthropic(ctx, t.BaseURL, t.Key)
	default:
		return nil, fmt.Errorf("unknown flavor %q", t.Flavor)
	}
	if err == nil && t.KeyCheckURL != "" && t.Key != "" {
		err = l.checkKey(ctx, t.KeyCheckURL, t.Key)
	}
	if err != nil {
		ce := connectErrorFor(err, t.BadKeyStatus)
		ce.Err = redactKey(ce.Err, t.Key)
		return nil, ce
	}
	if t.OllamaDetails {
		out = l.addOllamaDetails(ctx, t.BaseURL, t.Key, out)
	}
	if t.LMStudioDetails {
		out = l.addLMStudioDetails(ctx, t.BaseURL, t.Key, out)
	}
	if out == nil {
		out = []ListedModel{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// redactKey removes key from err's text. A provider's error can echo what it
// was sent, and the cause is logged; no log line may contain a key.
func redactKey(err error, key string) error {
	if key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "[redacted]"))
}

// maxOllamaDetails bounds the per-model lookups after an Ollama listing; a
// server on this computer holds a handful of models, not thousands.
const maxOllamaDetails = 200

// ollamaShow is the part of Ollama's POST /api/show answer read here.
type ollamaShow struct {
	Capabilities []string `json:"capabilities"`
}

// nativeRoot is a local server's address without the /v1 its
// OpenAI-compatible API sits under, where its native API lives.
func nativeRoot(baseURL string) string {
	return strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
}

// nativeRequest calls a local server's native API, with the connection's key
// when it has one, decoding the answer into into. It reports false for
// anything but a 200 with a JSON body.
func (l *Lister) nativeRequest(ctx context.Context, method, url, key string, body io.Reader, into any) bool {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return false
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := l.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(into) == nil
}

// addOllamaDetails asks Ollama's native API what each listed model can do,
// filling in tool and image support, and drops models that can't chat (an
// embedding model lacks "completion"). It sends the connection's key as the
// listing did, for an Ollama behind a proxy that checks one. It is best effort:
// a model whose lookup fails is kept with its capabilities unknown.
func (l *Lister) addOllamaDetails(ctx context.Context, baseURL, key string, models []ListedModel) []ListedModel {
	root := nativeRoot(baseURL)
	out := models[:0]
	for i, m := range models {
		var show ollamaShow
		body, _ := json.Marshal(map[string]string{"model": m.ID})
		if i < maxOllamaDetails && l.nativeRequest(ctx, http.MethodPost, root+"/api/show", key, bytes.NewReader(body), &show) && show.Capabilities != nil {
			if !slices.Contains(show.Capabilities, "completion") {
				continue
			}
			tools := slices.Contains(show.Capabilities, "tools")
			vision := slices.Contains(show.Capabilities, "vision")
			m.ToolSupport, m.VisionSupport = &tools, &vision
		}
		out = append(out, m)
	}
	return out
}

// lmStudioModel is one entry of LM Studio's native model list. Version 1
// (/api/v1/models) reports a key, a display name and a capabilities object;
// the older version 0 (/api/v0/models) an id and a capabilities list, with
// "vlm" as the type of a vision model.
type lmStudioModel struct {
	Key          string          `json:"key"`
	ID           string          `json:"id"`
	DisplayName  string          `json:"display_name"`
	Type         string          `json:"type"`
	Capabilities json.RawMessage `json:"capabilities"`
}

// addLMStudioDetails reads LM Studio's native model list, filling in names,
// tool and image support, and drops models that can't chat (embedding
// models). It is best effort: without a native list, the models are kept as
// listed.
func (l *Lister) addLMStudioDetails(ctx context.Context, baseURL, key string, models []ListedModel) []ListedModel {
	root := nativeRoot(baseURL)
	var native []lmStudioModel
	var v1 struct {
		Models []lmStudioModel `json:"models"`
	}
	var v0 struct {
		Data []lmStudioModel `json:"data"`
	}
	switch {
	case l.nativeRequest(ctx, http.MethodGet, root+"/api/v1/models", key, nil, &v1) && v1.Models != nil:
		native = v1.Models
	case l.nativeRequest(ctx, http.MethodGet, root+"/api/v0/models", key, nil, &v0) && v0.Data != nil:
		native = v0.Data
	default:
		return models
	}
	byID := make(map[string]lmStudioModel, len(native))
	for _, n := range native {
		id := n.Key
		if id == "" {
			id = n.ID
		}
		byID[id] = n
	}
	out := models[:0]
	for _, m := range models {
		n, ok := byID[m.ID]
		if !ok {
			out = append(out, m)
			continue
		}
		if n.Type != "llm" && n.Type != "vlm" {
			continue
		}
		if n.DisplayName != "" && n.DisplayName != m.ID {
			m.DisplayName = n.DisplayName
		}
		tools, vision := lmStudioCapabilities(n)
		m.ToolSupport, m.VisionSupport = &tools, &vision
		out = append(out, m)
	}
	return out
}

// lmStudioCapabilities reads tool and image support from either version of
// the native list.
func lmStudioCapabilities(n lmStudioModel) (tools, vision bool) {
	var asObject struct {
		Vision            bool `json:"vision"`
		TrainedForToolUse bool `json:"trained_for_tool_use"`
	}
	if json.Unmarshal(n.Capabilities, &asObject) == nil {
		return asObject.TrainedForToolUse, asObject.Vision || n.Type == "vlm"
	}
	var asList []string
	_ = json.Unmarshal(n.Capabilities, &asList)
	return slices.Contains(asList, "tool_use"), n.Type == "vlm"
}

// keyCheckError is a key check that was answered with something other than
// success.
type keyCheckError struct{ status int }

func (e *keyCheckError) Error() string { return fmt.Sprintf("key check answered %d", e.status) }

// checkKey fetches url with key as a bearer token; any success means the key is
// good.
func (l *Lister) checkKey(ctx context.Context, url, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := l.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &keyCheckError{status: resp.StatusCode}
	}
	return nil
}

func (l *Lister) listOpenAI(ctx context.Context, baseURL, key string) ([]ListedModel, error) {
	opts := []openaioption.RequestOption{
		openaioption.WithBaseURL(baseURL),
		openaioption.WithMaxRetries(0),
	}
	if l.httpClient != nil {
		opts = append(opts, openaioption.WithHTTPClient(l.httpClient))
	}
	if key != "" {
		opts = append(opts, openaioption.WithAPIKey(key))
	}
	svc := openai.NewModelService(opts...)
	var out []ListedModel
	iter := svc.ListAutoPaging(ctx)
	for len(out) < maxListModels && iter.Next() {
		m := iter.Current()
		out = append(out, listedFromOpenAI(m.ID, m.RawJSON()))
	}
	return out, iter.Err()
}

func (l *Lister) listAnthropic(ctx context.Context, baseURL, key string) ([]ListedModel, error) {
	opts := []anthropicoption.RequestOption{
		anthropicoption.WithBaseURL(baseURL),
		anthropicoption.WithMaxRetries(0),
	}
	if l.httpClient != nil {
		opts = append(opts, anthropicoption.WithHTTPClient(l.httpClient))
	}
	if key != "" {
		opts = append(opts, anthropicoption.WithAPIKey(key))
	}
	svc := anthropic.NewModelService(opts...)
	// Paged by hand rather than with the SDK's auto-pager, which keeps asking
	// while a page has a cursor and never checks that the cursor moved: a server
	// that ignores after_id would be asked for the same page until the timeout.
	var out []ListedModel
	params := anthropic.ModelListParams{}
	lastID := ""
	for pages := 0; pages < maxListPages; pages++ {
		page, err := svc.List(ctx, params)
		if err != nil {
			return out, err
		}
		for _, m := range page.Data {
			out = append(out, ListedModel{ID: m.ID, DisplayName: m.DisplayName})
		}
		// Go on only when the server says there is more and the cursor moved.
		if !page.HasMore || page.LastID == "" || page.LastID == lastID || len(out) >= maxListModels {
			break
		}
		lastID = page.LastID
		params.AfterID = anthropic.String(lastID)
	}
	if len(out) > maxListModels {
		out = out[:maxListModels]
	}
	return out, nil
}

// ErrInvalidAddress is returned for an address that is not an absolute http or
// https URL.
var ErrInvalidAddress = errors.New("address must be an http:// or https:// URL")

// normalizeAddress checks an address an account typed and trims it.
func normalizeAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", ErrInvalidAddress
	}
	return strings.TrimRight(raw, "/"), nil
}
