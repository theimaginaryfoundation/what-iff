package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// advancedTranscript is everything an Advanced run (ADR 0x026) writes, in order: native
// reasoning, the text it writes between tool calls, and earlier steps' replies. It streams live
// into the job's draft reasoning (with a header per step), and becomes the run's structured
// record (run): per step, its notes, minus the final call's reply text, which is the message.
//
// Calls are numbered by beginCall so a provider retry can discard what the current call streamed
// (discardCallNative) without losing earlier calls.
type advancedTranscript struct {
	mu   sync.Mutex
	segs []transcriptSegment
	call int
	// step is the run's current step (stepHeader); segments are filed under it.
	step int
	// out receives live deltas; reset clears what out has received (the whole transcript is
	// then re-sent). Either may be nil.
	out   func(delta string)
	reset func()
}

type transcriptKind int

const (
	transcriptNative transcriptKind = iota
	transcriptText
	// transcriptToolCall marks where a tool call happened; toolCallID is its ToolCall row.
	transcriptToolCall
)

type transcriptSegment struct {
	kind       transcriptKind
	call       int
	step       int
	text       string
	toolCallID string
}

// Size caps on a saved run (the live stream is not capped): a note keeps its first
// advancedNoteMaxRunes runes, and once a run's notes reach advancedRunMaxRunes later notes are
// dropped, with a marker saying so.
const (
	advancedNoteMaxRunes = 20_000
	advancedRunMaxRunes  = 400_000
)

func newAdvancedTranscript(out func(string), reset func()) *advancedTranscript {
	return &advancedTranscript{out: out, reset: reset, step: 1}
}

// beginCall marks the start of a model call.
func (t *advancedTranscript) beginCall() {
	t.mu.Lock()
	t.call++
	t.mu.Unlock()
}

// currentCall is the number of the latest call.
func (t *advancedTranscript) currentCall() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.call
}

// nativeDelta records streamed model reasoning.
func (t *advancedTranscript) nativeDelta(d string) { t.appendDelta(transcriptNative, d) }

// textDelta records streamed reply text.
func (t *advancedTranscript) textDelta(d string) { t.appendDelta(transcriptText, d) }

// stepHeader starts a step after the first: later notes are filed under it, and the live stream
// shows a header.
func (t *advancedTranscript) stepHeader(step int) {
	t.mu.Lock()
	t.step = step
	hasContent := len(t.segs) > 0
	out := t.out
	t.mu.Unlock()
	if out != nil {
		header := fmt.Sprintf("— Step %d —", step)
		if hasContent {
			header = "\n\n" + header
		}
		out(header)
	}
}

// toolCall records that a tool call (its ToolCall id) ran after the current call, so the saved
// run keeps notes and tool calls in the order they happened. It is not streamed: the live view
// shows tool calls from the job's progress timeline.
func (t *advancedTranscript) toolCall(id string) {
	t.mu.Lock()
	t.segs = append(t.segs, transcriptSegment{kind: transcriptToolCall, call: t.call, step: t.step, toolCallID: id})
	t.mu.Unlock()
}

func (t *advancedTranscript) appendDelta(kind transcriptKind, d string) {
	if d == "" {
		return
	}
	t.mu.Lock()
	var live string
	n := len(t.segs)
	if n > 0 && t.segs[n-1].kind == kind && t.segs[n-1].call == t.call {
		t.segs[n-1].text += d
		live = d
	} else {
		// A new segment starts on its own paragraph; providers' own leading separators are dropped.
		d = strings.TrimLeft(d, "\n")
		if d == "" {
			t.mu.Unlock()
			return
		}
		t.segs = append(t.segs, transcriptSegment{kind: kind, call: t.call, step: t.step, text: d})
		live = d
		if n > 0 {
			live = "\n\n" + d
		}
	}
	out := t.out
	t.mu.Unlock()
	if out != nil {
		out(live)
	}
}

// discardCallNative drops the reasoning the current call streamed (a provider retry or the
// truncation fallback discarded that attempt) and re-sends the live transcript.
func (t *advancedTranscript) discardCallNative() {
	t.mu.Lock()
	kept := t.segs[:0]
	for _, s := range t.segs {
		if s.kind == transcriptNative && s.call == t.call {
			continue
		}
		kept = append(kept, s)
	}
	t.segs = kept
	full := t.renderLocked()
	out, reset := t.out, t.reset
	t.mu.Unlock()
	if reset != nil {
		reset()
	}
	if out != nil && full != "" {
		out(full)
	}
}

// run is the run's structured record: per step, its events in order (reasoning, text and tool
// calls), without the reply text of finalCall (the message itself). Notes are capped by
// advancedNoteMaxRunes and advancedRunMaxRunes; tool-call events are always kept.
func (t *advancedTranscript) run(finalCall int) []models.AgentRunStep {
	t.mu.Lock()
	defer t.mu.Unlock()
	var steps []models.AgentRunStep
	total := 0
	capped := false
	for i := range t.segs {
		s := &t.segs[i]
		if s.kind == transcriptText && s.call == finalCall {
			continue
		}
		var ev models.AgentRunEvent
		switch s.kind {
		case transcriptToolCall:
			ev = models.AgentRunEvent{Kind: models.AgentRunEventToolCall, ToolCallID: s.toolCallID}
		default:
			text := strings.TrimSpace(s.text)
			if text == "" || capped {
				continue
			}
			if r := []rune(text); len(r) > advancedNoteMaxRunes {
				text = string(r[:advancedNoteMaxRunes]) + "\n…[truncated]"
			}
			ev = models.AgentRunEvent{Kind: models.AgentRunEventText, Text: text}
			if s.kind == transcriptNative {
				ev.Kind = models.AgentRunEventReasoning
			}
			total += len([]rune(text))
			if total > advancedRunMaxRunes {
				capped = true
				ev = models.AgentRunEvent{Kind: models.AgentRunEventText, Text: "…[later notes omitted: the run's notes exceeded the saved size]"}
			}
		}
		if len(steps) == 0 || steps[len(steps)-1].Step != s.step {
			steps = append(steps, models.AgentRunStep{Step: s.step})
		}
		cur := &steps[len(steps)-1]
		cur.Events = append(cur.Events, ev)
	}
	return steps
}

// textSince returns the reply text streamed in calls after afterCall, for the work log.
func (t *advancedTranscript) textSince(afterCall int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var parts []string
	for i := range t.segs {
		s := &t.segs[i]
		if s.kind == transcriptText && s.call > afterCall {
			if txt := strings.TrimSpace(s.text); txt != "" {
				parts = append(parts, txt)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// renderLocked rebuilds the live stream (for a re-send after a discard): segments in order, a
// header where the step changes.
func (t *advancedTranscript) renderLocked() string {
	var b strings.Builder
	step := 1
	for i := range t.segs {
		s := &t.segs[i]
		txt := strings.TrimSpace(s.text)
		if txt == "" {
			continue
		}
		if s.step != step {
			step = s.step
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			fmt.Fprintf(&b, "— Step %d —", step)
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(txt)
	}
	return b.String()
}
