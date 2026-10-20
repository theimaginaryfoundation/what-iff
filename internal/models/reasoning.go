package models

import (
	"context"
	"fmt"
	"strings"
)

// ReasoningEffort is how hard a turn asks the model to think (ADR 0x026). The zero value
// ("") means "not set": the model's native default, which is what every turn sent before
// the setting existed.
type ReasoningEffort string

const (
	ReasoningEffortOff    ReasoningEffort = "off"
	ReasoningEffortLow    ReasoningEffort = "low"
	ReasoningEffortMedium ReasoningEffort = "medium"
	ReasoningEffortHigh   ReasoningEffort = "high"
)

// ReasoningEfforts are the selectable levels, lowest first.
var ReasoningEfforts = []ReasoningEffort{ReasoningEffortOff, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}

// Valid reports whether e is one of the selectable levels (not "").
func (e ReasoningEffort) Valid() bool {
	switch e {
	case ReasoningEffortOff, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh:
		return true
	}
	return false
}

// AgentLoop selects how a turn drives tools (ADR 0x026). The zero value ("") means "not
// set", which runs the Standard loop.
type AgentLoop string

const (
	// AgentLoopStandard is the original tool loop: call the model until it stops asking
	// for tools, at most maxToolCallRounds rounds.
	AgentLoopStandard AgentLoop = "standard"
	// AgentLoopAdvanced is the long-horizon ReAct loop: steps of Standard loops, driven by
	// a plan, until the work is done.
	AgentLoopAdvanced AgentLoop = "advanced"
)

// Valid reports whether l is a selectable loop (not "").
func (l AgentLoop) Valid() bool {
	return l == AgentLoopStandard || l == AgentLoopAdvanced
}

// ReasoningSupport is how a model can take a ReasoningEffort.
type ReasoningSupport string

const (
	// ReasoningSupportNone: no effort control; the setting is ignored.
	ReasoningSupportNone ReasoningSupport = "none"
	// ReasoningSupportOptional: off plus the three levels.
	ReasoningSupportOptional ReasoningSupport = "optional"
	// ReasoningSupportToggle: thinking can only be switched on or off (Xiaomi MiMo).
	ReasoningSupportToggle ReasoningSupport = "toggle"
	// ReasoningSupportAlways: the model always reasons; levels but no off (z.ai GLM,
	// Claude Opus 5.5 and Fable).
	ReasoningSupportAlways ReasoningSupport = "always"
)

// ReasoningSupportFor reports how the model (by its row's provider and name) takes a
// reasoning effort. It is derived here rather than stored on the model row so models an
// admin adds are covered without a migration.
func ReasoningSupportFor(provider, name string) ReasoningSupport {
	n := normalizeModelName(name)
	switch providerForModel(provider, name) {
	case ModelProviderOpenAI:
		return openAIReasoningSupport(n)
	case ModelProviderAnthropic:
		return anthropicReasoningSupport(n)
	case ModelProviderZAI:
		return ReasoningSupportAlways
	case ModelProviderXiaomi:
		return ReasoningSupportToggle
	case ModelProviderGoogle:
		return geminiReasoningSupport(n)
	case ModelProviderQwen:
		return qwenReasoningSupport(n)
	case ModelProviderDeepSeek:
		return deepSeekReasoningSupport(n)
	case ModelProviderMistral:
		return mistralReasoningSupport(n)
	}
	return ReasoningSupportNone
}

// EffectiveReasoningEffort maps a selected effort onto what the model can do. It returns ""
// when nothing should be sent: the effort is not set, or the model has no effort control.
//   - toggle models (on/off only): every level is "on", reported as medium.
//   - always-on models: off is not possible and runs as low, the closest to what was asked.
func EffectiveReasoningEffort(support ReasoningSupport, effort ReasoningEffort) ReasoningEffort {
	if !effort.Valid() {
		return ""
	}
	switch support {
	case ReasoningSupportOptional:
		return effort
	case ReasoningSupportToggle:
		if effort == ReasoningEffortOff {
			return ReasoningEffortOff
		}
		return ReasoningEffortMedium
	case ReasoningSupportAlways:
		if effort == ReasoningEffortOff {
			return ReasoningEffortLow
		}
		return effort
	}
	return ""
}

// SettingSource names the level a turn setting came from.
type SettingSource string

const (
	SettingSourceUnset  SettingSource = ""
	SettingSourceThread SettingSource = "thread"
	SettingSourceMode   SettingSource = "mode"
	SettingSourceJob    SettingSource = "job"
)

// TurnSettings are a turn's resolved reasoning effort and agent loop, with the level each
// came from. A zero ReasoningEffort means "not set"; a zero AgentLoop runs Standard.
type TurnSettings struct {
	ReasoningEffort       ReasoningEffort
	ReasoningEffortSource SettingSource
	AgentLoop             AgentLoop
	AgentLoopSource       SettingSource
}

// Advanced reports whether the turn runs the Advanced loop.
func (s TurnSettings) Advanced() bool { return s.AgentLoop == AgentLoopAdvanced }

// TurnSettingLevel is one level's stored values; nil means "not set at this level".
type TurnSettingLevel struct {
	ReasoningEffort *ReasoningEffort
	AgentLoop       *AgentLoop
}

// ResolveTurnSettings applies ADR 0x026's precedence for a turn: the job's values win, then
// the active mode's, then the thread's (which were seeded from the personality or account
// default when the thread was created). Invalid stored values are skipped.
func ResolveTurnSettings(thread, mode, job TurnSettingLevel) TurnSettings {
	var s TurnSettings
	for _, lvl := range []struct {
		values TurnSettingLevel
		source SettingSource
	}{{job, SettingSourceJob}, {mode, SettingSourceMode}, {thread, SettingSourceThread}} {
		if s.ReasoningEffortSource == SettingSourceUnset && lvl.values.ReasoningEffort != nil && lvl.values.ReasoningEffort.Valid() {
			s.ReasoningEffort = *lvl.values.ReasoningEffort
			s.ReasoningEffortSource = lvl.source
		}
		if s.AgentLoopSource == SettingSourceUnset && lvl.values.AgentLoop != nil && lvl.values.AgentLoop.Valid() {
			s.AgentLoop = *lvl.values.AgentLoop
			s.AgentLoopSource = lvl.source
		}
	}
	return s
}

// FirstSetReasoningEffort returns the first valid value, for seeding a new thread from its
// personality and then the account default. Nil when none is set.
func FirstSetReasoningEffort(values ...*ReasoningEffort) *ReasoningEffort {
	for _, v := range values {
		if v != nil && v.Valid() {
			out := *v
			return &out
		}
	}
	return nil
}

// FirstSetAgentLoop is FirstSetReasoningEffort for the agent loop.
func FirstSetAgentLoop(values ...*AgentLoop) *AgentLoop {
	for _, v := range values {
		if v != nil && v.Valid() {
			out := *v
			return &out
		}
	}
	return nil
}

// ParseReasoningEffortUpdate reads an update field: nil leaves the value alone (ok, no
// change), "" clears it, and a level sets it. ok is false for an unknown value, which the caller
// must reject as a client error (400): treating it as "no change" would silently drop the request.
func ParseReasoningEffortUpdate(raw *string) (value *ReasoningEffort, change bool, ok bool) {
	if raw == nil {
		return nil, false, true
	}
	v := ReasoningEffort(strings.ToLower(strings.TrimSpace(*raw)))
	if v == "" {
		return nil, true, true
	}
	if !v.Valid() {
		return nil, false, false
	}
	return &v, true, true
}

// ParseAgentLoopUpdate is ParseReasoningEffortUpdate for the agent loop, with the same contract:
// ok=false is a 400.
func ParseAgentLoopUpdate(raw *string) (value *AgentLoop, change bool, ok bool) {
	if raw == nil {
		return nil, false, true
	}
	v := AgentLoop(strings.ToLower(strings.TrimSpace(*raw)))
	if v == "" {
		return nil, true, true
	}
	if !v.Valid() {
		return nil, false, false
	}
	return &v, true, true
}

// TurnSettingValues is one level's reasoning effort and agent loop as carried by the API
// (embedded in the preference, personality, mood, chat and agent-job DTOs). JSON null, or an
// omitted field, is "not set"; on update routes an empty string clears the level.
type TurnSettingValues struct {
	ReasoningEffort *ReasoningEffort `json:"reasoning_effort"`
	AgentLoop       *AgentLoop       `json:"agent_loop"`
}

// Validate rejects unknown values. Empty strings are allowed (they clear).
func (v TurnSettingValues) Validate() error {
	if v.ReasoningEffort != nil && *v.ReasoningEffort != "" && !v.ReasoningEffort.Valid() {
		return fmt.Errorf("invalid reasoning_effort %q: must be one of off, low, medium, high", *v.ReasoningEffort)
	}
	if v.AgentLoop != nil && *v.AgentLoop != "" && !v.AgentLoop.Valid() {
		return fmt.Errorf("invalid agent_loop %q: must be one of standard, advanced", *v.AgentLoop)
	}
	return nil
}

// Normalized returns the values with empty strings turned into nil ("not set"). It also nils any
// unknown value, so on user input call Validate first (the handlers do) and use Normalized only to
// turn the validated "" into "not set"; on stored or imported values it repairs rather than rejects.
func (v TurnSettingValues) Normalized() TurnSettingValues {
	out := v
	if out.ReasoningEffort != nil && !out.ReasoningEffort.Valid() {
		out.ReasoningEffort = nil
	}
	if out.AgentLoop != nil && !out.AgentLoop.Valid() {
		out.AgentLoop = nil
	}
	return out
}

// Level returns the values as a resolution level.
func (v TurnSettingValues) Level() TurnSettingLevel {
	n := v.Normalized()
	return TurnSettingLevel{ReasoningEffort: n.ReasoningEffort, AgentLoop: n.AgentLoop}
}

type jobTurnSettingsKey struct{}

// WithJobTurnSettings attaches an agent job's turn settings to a run's context. They override
// the thread and mode for that run only (ADR 0x026); the scheduler sets them, the agent reads
// them when it resolves the turn. Carried on the context rather than through the run's signature
// because several interfaces sit between the two.
func WithJobTurnSettings(ctx context.Context, v TurnSettingValues) context.Context {
	return context.WithValue(ctx, jobTurnSettingsKey{}, v)
}

// JobTurnSettingsFromContext returns the job's turn settings, or an empty level when none.
func JobTurnSettingsFromContext(ctx context.Context) TurnSettingLevel {
	if ctx == nil {
		return TurnSettingLevel{}
	}
	v, _ := ctx.Value(jobTurnSettingsKey{}).(TurnSettingValues)
	return v.Level()
}

// CopyJobTurnSettings carries from's job turn settings, if any, onto to (for runs that detach
// from their caller's context).
func CopyJobTurnSettings(from, to context.Context) context.Context {
	if from == nil {
		return to
	}
	if v, ok := from.Value(jobTurnSettingsKey{}).(TurnSettingValues); ok {
		return context.WithValue(to, jobTurnSettingsKey{}, v)
	}
	return to
}

// TurnSettingValuesFromStrings reads stored or exported strings; empty or unknown values are
// "not set".
func TurnSettingValuesFromStrings(effort, loop string) TurnSettingValues {
	e, l := ReasoningEffort(effort), AgentLoop(loop)
	return TurnSettingValues{ReasoningEffort: &e, AgentLoop: &l}.Normalized()
}

// Strings returns the values as strings, "" for not set (for formats that store plain text).
func (v TurnSettingValues) Strings() (effort, loop string) {
	n := v.Normalized()
	if n.ReasoningEffort != nil {
		effort = string(*n.ReasoningEffort)
	}
	if n.AgentLoop != nil {
		loop = string(*n.AgentLoop)
	}
	return effort, loop
}
