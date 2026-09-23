package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeProgressWriter struct {
	mu       sync.Mutex
	payloads []string
	err      error
}

func (f *fakeProgressWriter) UpdateJobProgress(_ context.Context, _, _ uuid.UUID, progress string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payloads = append(f.payloads, progress)
	return f.err
}

func (f *fakeProgressWriter) decoded(t *testing.T) []models.ChatTurnProgress {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]models.ChatTurnProgress, len(f.payloads))
	for i, p := range f.payloads {
		require.NoError(t, json.Unmarshal([]byte(p), &out[i]))
	}
	return out
}

func testChatJob() *models.Job {
	return &models.Job{ID: uuid.New(), UserID: uuid.New(), JobType: "chat_message"}
}

func TestJobToolProgress_StartedThenFinished(t *testing.T) {
	w := &fakeProgressWriter{}
	p := newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), nil)
	require.NotNil(t, p)
	clock := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { clock = clock.Add(time.Second); return clock }

	p.Started(1, provider.ToolUse{ID: "call-1", Name: "list_memories", Input: []byte(`{"q":"fox"}`)})
	p.Finished(provider.ToolResult{ID: "call-1", Output: `{"items":[]}`})

	snaps := w.decoded(t)
	require.Len(t, snaps, 2)

	running := snaps[0].ToolCalls
	require.Len(t, running, 1)
	assert.Equal(t, "call-1", running[0].ID)
	assert.Equal(t, "list_memories", running[0].Name)
	assert.Equal(t, `{"q":"fox"}`, running[0].Input)
	assert.Equal(t, models.ChatTurnToolRunning, running[0].Status)
	assert.Equal(t, 1, running[0].Round)
	assert.Nil(t, running[0].FinishedAt)

	done := snaps[1].ToolCalls
	require.Len(t, done, 1)
	assert.Equal(t, models.ChatTurnToolComplete, done[0].Status)
	assert.Equal(t, `{"items":[]}`, done[0].Output)
	require.NotNil(t, done[0].FinishedAt)
	assert.True(t, done[0].FinishedAt.After(done[0].StartedAt))
}

func TestJobToolProgress_ErrorResultAndOrdering(t *testing.T) {
	w := &fakeProgressWriter{}
	p := newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), nil)

	p.Started(0, provider.ToolUse{ID: "a", Name: "first"})
	p.Finished(provider.ToolResult{ID: "a", Output: "ok"})
	p.Started(0, provider.ToolUse{ID: "b", Name: "second"})
	p.Finished(provider.ToolResult{ID: "b", Output: "boom", IsErr: true})

	snaps := w.decoded(t)
	require.Len(t, snaps, 4)
	final := snaps[3].ToolCalls
	require.Len(t, final, 2)
	assert.Equal(t, []string{"first", "second"}, []string{final[0].Name, final[1].Name})
	assert.Equal(t, models.ChatTurnToolComplete, final[0].Status)
	assert.Equal(t, models.ChatTurnToolError, final[1].Status)
	assert.Equal(t, "boom", final[1].Output)
}

func TestJobToolProgress_FinishedForUnknownCallIsIgnored(t *testing.T) {
	w := &fakeProgressWriter{}
	p := newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), nil)
	p.Finished(provider.ToolResult{ID: "never-started", Output: "x"})
	assert.Empty(t, w.payloads)
}

func TestJobToolProgress_TruncatesPreviewsRuneSafely(t *testing.T) {
	w := &fakeProgressWriter{}
	p := newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), nil)
	long := strings.Repeat("🦊", toolProgressPreviewRunes+50)

	p.Started(0, provider.ToolUse{ID: "a", Name: "big", Input: []byte(long)})
	p.Finished(provider.ToolResult{ID: "a", Output: long})

	final := w.decoded(t)[1].ToolCalls[0]
	for _, s := range []string{final.Input, final.Output} {
		assert.True(t, utf8.ValidString(s))
		assert.LessOrEqual(t, utf8.RuneCountInString(s), toolProgressPreviewRunes+1) // + ellipsis
		assert.True(t, strings.HasSuffix(s, "…"))
	}
}

func TestJobToolProgress_FlushesBufferedTextBeforeRecordingStart(t *testing.T) {
	w := &fakeProgressWriter{}
	var order []string
	p := newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), func() {
		w.mu.Lock()
		order = append(order, "flush", "writes-so-far:"+string(rune('0'+len(w.payloads))))
		w.mu.Unlock()
	})
	p.Started(0, provider.ToolUse{ID: "a", Name: "t"})
	assert.Equal(t, []string{"flush", "writes-so-far:0"}, order)
	assert.Len(t, w.payloads, 1)
}

func TestJobToolProgress_WriteFailureIsBestEffort(t *testing.T) {
	w := &fakeProgressWriter{err: errors.New("db down")}
	p := newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), nil)
	assert.NotPanics(t, func() {
		p.Started(0, provider.ToolUse{ID: "a", Name: "t"})
		p.Finished(provider.ToolResult{ID: "a", Output: "ok"})
	})
	assert.Len(t, w.payloads, 2)
}

func TestJobToolProgress_NilSafety(t *testing.T) {
	var p *jobToolProgress
	assert.NotPanics(t, func() {
		p.Started(0, provider.ToolUse{ID: "a"})
		p.Finished(provider.ToolResult{ID: "a"})
	})
	assert.Nil(t, newJobToolProgress(context.Background(), &fakeProgressWriter{}, nil, nil, nil))
	assert.Nil(t, newJobToolProgress(context.Background(), nil, nil, testChatJob(), nil))
	assert.Nil(t, (&Agent{logger: zap.NewNop()}).newChatToolProgress(testChatJob(), nil), "nil datastore must not become a non-nil interface")
}

func TestExecuteToolUses_RecordsLiveToolProgress(t *testing.T) {
	prevExtra := extraToolHandlersForChat
	t.Cleanup(func() { extraToolHandlersForChat = prevExtra })
	extraToolHandlersForChat = func(_ *Agent, _ *models.Chat) map[string]ExtraToolHandler {
		return map[string]ExtraToolHandler{
			"ok_tool": func(context.Context, []byte) (string, []*models.FileAttachment, error) {
				return `{"ok":true}`, nil, nil
			},
			"bad_tool": func(context.Context, []byte) (string, []*models.FileAttachment, error) {
				return "", nil, errors.New("nope")
			},
		}
	}

	w := &fakeProgressWriter{}
	a := &Agent{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}
	chatCtx := &chatContext{chat: chat, toolProgress: newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), nil)}

	a.executeToolUses(context.Background(), chatCtx, 2, []provider.ToolUse{
		{ID: "u1", Name: "ok_tool", Input: []byte(`{}`)},
		{ID: "u2", Name: "bad_tool", Input: []byte(`{"x":1}`)},
	})

	snaps := w.decoded(t)
	require.Len(t, snaps, 4, "one write per start and per finish")
	assert.Equal(t, models.ChatTurnToolRunning, snaps[0].ToolCalls[0].Status)
	final := snaps[3].ToolCalls
	require.Len(t, final, 2)
	assert.Equal(t, models.ChatTurnToolComplete, final[0].Status)
	assert.Equal(t, `{"ok":true}`, final[0].Output)
	assert.Equal(t, models.ChatTurnToolError, final[1].Status)
	assert.Contains(t, final[1].Output, "nope")
	assert.Equal(t, 2, final[1].Round)
}
