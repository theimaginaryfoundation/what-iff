package relaykit

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Defaults of the turn policy (Policy). See docs/adr/0x031-relay-turn-batching-and-loop-guard.md.
const (
	// DefaultAgentMaxDepth is how many turns in a row agents may start on a binding before a
	// person has to speak again.
	DefaultAgentMaxDepth = 3
	// DefaultAgentTurnsPerHour bounds the turns agents start on one binding in an hour.
	DefaultAgentTurnsPerHour = 20
	// DefaultChainGap is how long a quiet binding keeps its agent depth; after it, an agent's
	// message starts a new exchange.
	DefaultChainGap = 30 * time.Minute
)

// Policy is how a relay paces its turns, the same for Discord and Buzz. Zero fields take
// their defaults.
type Policy struct {
	// BatchMax bounds the messages waiting per relay thread (TurnQueue.Max).
	BatchMax int
	// AgentMaxDepth caps consecutive agent-started turns per binding (LoopGuard.MaxDepth).
	AgentMaxDepth int
	// AgentTurnsPerHour bounds agent-started turns per binding (LoopGuard.TurnsPerHour).
	AgentTurnsPerHour int
}

// PolicyFromEnv reads RELAY_BATCH_MAX, RELAY_AGENT_MAX_DEPTH and RELAY_AGENT_TURNS_PER_HOUR.
// A missing or invalid value keeps the default (an invalid one is logged).
func PolicyFromEnv(logger *zap.Logger) Policy {
	read := func(name string) int {
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			return 0
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			if logger != nil {
				logger.Warn("relay: ignoring invalid setting", zap.String("name", name), zap.String("value", v))
			}
			return 0
		}
		return n
	}
	return Policy{
		BatchMax:          read("RELAY_BATCH_MAX"),
		AgentMaxDepth:     read("RELAY_AGENT_MAX_DEPTH"),
		AgentTurnsPerHour: read("RELAY_AGENT_TURNS_PER_HOUR"),
	}
}

// Guard is the loop guard the policy describes.
func (p Policy) Guard() *LoopGuard {
	return &LoopGuard{MaxDepth: p.AgentMaxDepth, TurnsPerHour: p.AgentTurnsPerHour}
}

// LastInbound is the newest message recorded on a binding, as the loop guard needs it: its
// agent depth (persisted on the message link) and when it came.
type LastInbound struct {
	Depth int
	At    time.Time
}

// LoopGuard bounds conversations between agents (other personas, other bots) on a binding.
//
// Every inbound message gets an agent depth, stored on its message link so a restart does not
// reset it: 0 from a person; from an agent, one more than the binding's previous message (or
// 1 when the binding was quiet for ChainGap). A person speaking resets the chain. The guard
// answers agents up to MaxDepth deep, asks the persona to wrap up as the cap nears, and
// bounds agent-started turns per binding per hour as a backstop.
//
// The chain is the bound channel, not a thread: an agent cannot escape it by starting a new
// thread, and anyone tagging the persona there resets it.
type LoopGuard struct {
	// MaxDepth is the deepest agent message answered (DefaultAgentMaxDepth when zero).
	MaxDepth int
	// TurnsPerHour bounds agent-started turns per binding (DefaultAgentTurnsPerHour when zero).
	TurnsPerHour int
	// ChainGap is how long a quiet binding keeps its depth (DefaultChainGap when zero).
	ChainGap time.Duration
	// Clock is the time source (RealClock when nil).
	Clock Clock

	mu sync.Mutex
	// turns holds each binding's agent-started turns of the past hour; its keys are bounded
	// by the bindings this process serves.
	turns map[uuid.UUID][]time.Time
	// dropped is when each binding last dropped an agent's message past the cap. Dropped
	// messages are not recorded, so without this an agent that keeps posting would start a new
	// chain ChainGap after the last answered one; with it, the chain stays closed while the
	// agent keeps trying. Kept in memory: a restart reopens the chain once, within the hourly
	// budget.
	dropped map[uuid.UUID]time.Time
}

// effective is prev as the chain stands: a drop newer than the binding's last recorded message
// counts as a message at the cap.
func (g *LoopGuard) effective(bindingID uuid.UUID, prev *LastInbound) *LastInbound {
	g.mu.Lock()
	at, ok := g.dropped[bindingID]
	g.mu.Unlock()
	if !ok || (prev != nil && !at.After(prev.At)) {
		return prev
	}
	return &LastInbound{Depth: g.maxDepth(), At: at}
}

func (g *LoopGuard) noteDropped(bindingID uuid.UUID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dropped == nil {
		g.dropped = map[uuid.UUID]time.Time{}
	}
	g.dropped[bindingID] = g.clock().Now()
}

// Closed reports whether an agent's message on the binding would be dropped now, given its
// previous message (nil: none within Since). Relays check it before queueing an agent's
// message, so an agent past the cap takes no place in the queue from the people waiting.
// A drop it causes extends the chain like one in Batch.
func (g *LoopGuard) Closed(bindingID uuid.UUID, prev *LastInbound) bool {
	if g.Allows(g.Depth(true, g.effective(bindingID, prev))) {
		return false
	}
	g.noteDropped(bindingID)
	return true
}

// Saturated stands in for the previous message when it could not be read: the chain counts
// as at its cap, so agents are dropped (people are still answered) rather than a failed read
// restarting an exchange.
func (g *LoopGuard) Saturated() *LastInbound {
	return &LastInbound{Depth: g.maxDepth(), At: g.clock().Now()}
}

func (g *LoopGuard) clock() Clock {
	if g.Clock == nil {
		return RealClock
	}
	return g.Clock
}

func (g *LoopGuard) maxDepth() int {
	if g.MaxDepth <= 0 {
		return DefaultAgentMaxDepth
	}
	return g.MaxDepth
}

// Depth is the agent depth of a new message, given the binding's previous one (nil: none).
func (g *LoopGuard) Depth(fromAgent bool, prev *LastInbound) int {
	if !fromAgent {
		return 0
	}
	gap := g.ChainGap
	if gap <= 0 {
		gap = DefaultChainGap
	}
	if prev == nil || g.clock().Now().Sub(prev.At) > gap {
		return 1
	}
	return prev.Depth + 1
}

// Since is how far back the binding's previous message counts for Depth: older ones start a
// new exchange, so a store need not look further.
func (g *LoopGuard) Since() time.Time {
	gap := g.ChainGap
	if gap <= 0 {
		gap = DefaultChainGap
	}
	return g.clock().Now().Add(-gap)
}

// Allows reports whether a message at depth may be answered.
func (g *LoopGuard) Allows(depth int) bool { return depth <= g.maxDepth() }

// Hint is the note added to a turn at depth, asking the persona to wrap up as the cap nears;
// "" when the turn is not near it (or was started by a person).
func (g *LoopGuard) Hint(depth int) string {
	maxDepth := g.maxDepth()
	switch {
	case depth <= 0 || depth < maxDepth-1:
		return ""
	case depth >= maxDepth:
		return "(Relay note: you are in an exchange with other agents, and this is your last reply in it. " +
			"Wrap it up: do not ask a question or invite another reply. " +
			"Agents' messages here are not delivered to you again until a person speaks.)"
	default:
		return "(Relay note: you have been going back and forth with other agents. " +
			fmt.Sprintf("Start wrapping up: you can reply to them %d more time after this before a person has to speak.)", maxDepth-depth)
	}
}

// Batch applies the guard to a batch's messages, oldest first, given whether each is from an
// agent and the binding's previous message (nil: none within Since). Each message gets its
// depth, or -1 when it is past the cap and must be dropped. Every kept message is recorded
// as the batch runs, so it is the next one's previous message as of now (its link's
// created_at), not as of when it arrived. turnDepth is 0 when a person is in the batch (a
// person's turn), and otherwise the deepest kept depth, which is where the chain stands after
// this turn and so what the wrap-up hint must count from. ok is false when nothing is left to
// answer, or when only agents are left and the binding has spent its hourly budget
// (TakeAgentTurn).
func (g *LoopGuard) Batch(bindingID uuid.UUID, fromAgent []bool, prev *LastInbound) (depths []int, turnDepth int, ok bool) {
	prev = g.effective(bindingID, prev)
	deepest, person := -1, false
	for _, agent := range fromAgent {
		d := g.Depth(agent, prev)
		if !g.Allows(d) {
			depths = append(depths, -1)
			g.noteDropped(bindingID)
			continue
		}
		depths = append(depths, d)
		prev = &LastInbound{Depth: d, At: g.clock().Now()}
		person = person || d == 0
		deepest = max(deepest, d)
	}
	if deepest < 0 {
		return depths, 0, false
	}
	turnDepth = deepest
	if person {
		turnDepth = 0
	}
	if turnDepth > 0 && !g.TakeAgentTurn(bindingID) {
		return depths, 0, false
	}
	return depths, turnDepth, true
}

// TakeAgentTurn records a turn started by agents on a binding, and reports false (recording
// nothing) when the binding already had TurnsPerHour of them in the past hour.
func (g *LoopGuard) TakeAgentTurn(bindingID uuid.UUID) bool {
	limit := g.TurnsPerHour
	if limit <= 0 {
		limit = DefaultAgentTurnsPerHour
	}
	now := g.clock().Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.turns == nil {
		g.turns = map[uuid.UUID][]time.Time{}
	}
	recent := g.turns[bindingID][:0]
	for _, at := range g.turns[bindingID] {
		if now.Sub(at) < time.Hour {
			recent = append(recent, at)
		}
	}
	if len(recent) >= limit {
		g.turns[bindingID] = recent
		return false
	}
	g.turns[bindingID] = append(recent, now)
	return true
}

// BatchPrompt is the user message of a turn: the messages' relayed lines, oldest first. A
// batch of several opens with a note that they came together and get one reply, which is
// posted in reply to the last of them; the persona may answer several at once, or only those
// worth answering.
func BatchPrompt(lines []string) string {
	if len(lines) == 1 {
		return lines[0]
	}
	return fmt.Sprintf("(Relay note: %d messages arrived while you were replying. "+
		"Write one reply to all of them: answer several at once, or only those worth answering; you need not answer each. "+
		"It is posted once, as a reply to the last message, so address anyone else by name.)\n", len(lines)) +
		strings.Join(lines, "\n")
}
