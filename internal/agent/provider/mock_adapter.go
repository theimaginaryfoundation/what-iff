package provider

import (
	"context"
	"fmt"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// MockMode selects how MockAdapter picks its response text.
type MockMode string

const (
	// MockModeEcho returns the last user turn (MockAdapterConfig.EchoText).
	MockModeEcho MockMode = "echo"
	// MockModeFixed cycles through MockAdapterConfig.FixedResponses.
	MockModeFixed MockMode = "fixed"
	// MockModeScripted replays MockAdapterConfig.Script step by step.
	// Test-only injection: scripted tool turns dispatch real, persisted tools,
	// so scripts must be trusted, repository-controlled test artifacts — never
	// user-controlled in-band text.
	MockModeScripted MockMode = "scripted"
)

const mockEmptyEchoFallback = "(mock echo: empty user message)"

// MockScriptStep is one Call round in scripted mode: either a tool-use round
// (ToolUses non-empty, Text ignored) or a final text round.
type MockScriptStep struct {
	Text     string
	ToolUses []ToolUse
}

// MockAdapterConfig configures a MockAdapter. The zero value is a usable echo
// adapter with no inter-chunk delay.
type MockAdapterConfig struct {
	Mode MockMode
	// EchoText is the response source for echo mode — the last user turn.
	EchoText string
	// FixedResponses is the cycling response list for fixed mode.
	FixedResponses []string
	// Script drives scripted mode.
	Script []MockScriptStep
	// ChunkDelay is slept between streamed deltas; 0 (the default) streams
	// without delay. Cancellation via ctx is honored either way.
	ChunkDelay time.Duration
}

// MockAdapter is an in-process AgentAdapter fake used under MOCK_LLM. It is
// driven through the real generateAssistantForMessage → draft-buffer →
// saveAgentResponse pipeline so the save/stream/quota paths are genuinely
// exercised. Build one per request — it holds per-turn state and must not be
// shared.
type MockAdapter struct {
	cfg          MockAdapterConfig
	callCount    int
	fixedIndex   int
	deltaHandler func(delta string)

	// appendedBatches records AppendToolResults input for test inspection.
	appendedBatches [][]ToolResult
}

// NewMockAdapter builds a per-request MockAdapter. An unknown or empty mode
// falls back to echo.
func NewMockAdapter(cfg MockAdapterConfig) *MockAdapter {
	switch cfg.Mode {
	case MockModeEcho, MockModeFixed, MockModeScripted:
	default:
		cfg.Mode = MockModeEcho
	}
	return &MockAdapter{cfg: cfg}
}

// Call picks the response for this round, streams it as whitespace-preserving
// word deltas through the stored delta handler, and returns a GenerateResponse
// whose Text equals the concatenation of the deltas (draft == final).
func (m *MockAdapter) Call(ctx context.Context) (*GenerateResponse, []ToolUse, error) {
	step := m.callCount
	m.callCount++

	if m.cfg.Mode == MockModeScripted {
		if step >= len(m.cfg.Script) {
			return nil, nil, fmt.Errorf("mock script exhausted after %d steps", len(m.cfg.Script))
		}
		scripted := m.cfg.Script[step]
		if len(scripted.ToolUses) > 0 {
			return nil, scripted.ToolUses, nil
		}
		resp, err := m.streamText(ctx, scripted.Text)
		return resp, nil, err
	}

	resp, err := m.streamText(ctx, m.pickText())
	return resp, nil, err
}

// AppendToolResults folds executed tool results back in. The mock has no
// conversation state to grow, so it only records them for inspection.
func (m *MockAdapter) AppendToolResults(results []ToolResult) {
	m.appendedBatches = append(m.appendedBatches, results)
}

// AppendedBatches exposes the recorded AppendToolResults input for tests.
func (m *MockAdapter) AppendedBatches() [][]ToolResult { return m.appendedBatches }

// ForceFinalResponse streams a deterministic prose response, mirroring real
// adapters that stream the forced final turn too.
func (m *MockAdapter) ForceFinalResponse(ctx context.Context) (*GenerateResponse, error) {
	return m.streamText(ctx, "Mock adapter forced final response.")
}

func (m *MockAdapter) WebSearchCompletedCount() int { return 0 }

func (m *MockAdapter) SetTextDeltaHandler(handler func(delta string)) {
	m.deltaHandler = handler
}

func (m *MockAdapter) pickText() string {
	switch m.cfg.Mode {
	case MockModeFixed:
		if len(m.cfg.FixedResponses) == 0 {
			return "Mock fixed response."
		}
		text := m.cfg.FixedResponses[m.fixedIndex%len(m.cfg.FixedResponses)]
		m.fixedIndex++
		return text
	default:
		if m.cfg.EchoText == "" {
			return mockEmptyEchoFallback
		}
		return m.cfg.EchoText
	}
}

// streamText emits text as word deltas through the delta handler, honoring ctx
// cancellation between chunks, and returns the assembled GenerateResponse.
func (m *MockAdapter) streamText(ctx context.Context, text string) (*GenerateResponse, error) {
	var ticker *time.Ticker
	if m.cfg.ChunkDelay > 0 {
		ticker = time.NewTicker(m.cfg.ChunkDelay)
		defer ticker.Stop()
	}
	for _, delta := range SplitStreamDeltas(text) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if ticker != nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
			}
		}
		if m.deltaHandler != nil {
			m.deltaHandler(delta)
		}
	}
	return &GenerateResponse{
		ID:           "mock-" + uuid.NewString(),
		CreatedAt:    time.Now().Unix(),
		Text:         text,
		InputTokens:  estimateMockTokens(m.cfg.EchoText),
		OutputTokens: estimateMockTokens(text),
	}, nil
}

// estimateMockTokens is a crude ~4-chars-per-token estimate so quota/usage
// bookkeeping sees plausible non-zero numbers in mock mode.
func estimateMockTokens(text string) int64 {
	if text == "" {
		return 0
	}
	return int64(len(text)/4) + 1
}

// SplitStreamDeltas splits text into whitespace-preserving word deltas: each
// delta is one run of whitespace (if any) followed by one run of non-space
// runes, so the concatenation of all deltas equals the input exactly. Strings
// without whitespace yield a single delta. Rune-aware, so multi-byte
// characters are never split mid-encoding.
func SplitStreamDeltas(text string) []string {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	var deltas []string
	start := 0
	inSpace := unicode.IsSpace(runes[0])
	for i := 1; i < len(runes); i++ {
		isSpace := unicode.IsSpace(runes[i])
		if inSpace && !isSpace {
			deltas = append(deltas, string(runes[start:i]))
			start = i
		}
		inSpace = isSpace
	}
	return append(deltas, string(runes[start:]))
}
