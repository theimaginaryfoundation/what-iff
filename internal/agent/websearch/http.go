package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxErrorMessage bounds how much of a provider error is echoed into errors (and from there
// into tool output), so a verbose upstream error can't flood the model's context.
// maxErrorBody bounds how much of the body is read to build it.
const (
	maxErrorMessage = 300
	maxErrorBody    = 16 << 10
)

func postJSON(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return do(client, req, headers, out)
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	return do(client, req, headers, out)
}

func do(client *http.Client, req *http.Request, headers map[string]string, out any) error {
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, summarizeErrorBody(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// providerErrorEnvelope covers the JSON error shapes of the supported providers: Parallel
// puts a message plus field-level validation errors under error; Brave puts a detail
// string there.
type providerErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Detail  any    `json:"detail"`
	} `json:"error"`
}

// summarizeErrorBody renders an error response as one readable line: the provider's
// message and any field errors when it is a known JSON envelope, otherwise the raw text.
// Either way the result is bounded and cut on a rune boundary, never mid-escape.
func summarizeErrorBody(body []byte) string {
	msg := strings.TrimSpace(string(body))
	var env providerErrorEnvelope
	if json.Unmarshal(body, &env) == nil {
		parts := []string{}
		if m := strings.TrimSpace(env.Error.Message); m != "" {
			parts = append(parts, m)
		}
		switch d := env.Error.Detail.(type) {
		case string:
			if d = strings.TrimSpace(d); d != "" {
				parts = append(parts, d)
			}
		case map[string]any:
			parts = append(parts, fieldErrors(d["errors"])...)
		}
		if len(parts) > 0 {
			msg = strings.Join(parts, " ")
		}
	}
	out, _ := truncateRunes(msg, maxErrorMessage)
	return out
}

// fieldErrors formats pydantic-style validation errors ({loc: [...], msg}) as "loc: msg".
func fieldErrors(raw any) []string {
	list, _ := raw.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		e, _ := item.(map[string]any)
		msg, _ := e["msg"].(string)
		if msg == "" {
			continue
		}
		loc := []string{}
		if parts, ok := e["loc"].([]any); ok {
			for _, p := range parts {
				loc = append(loc, fmt.Sprint(p))
			}
		}
		if len(loc) > 0 {
			msg = strings.Join(loc, ".") + ": " + msg
		}
		out = append(out, msg)
	}
	return out
}
