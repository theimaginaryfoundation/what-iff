package agent

import (
	"context"
	"encoding/json"
	"strings"

	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// askUserTool puts the model's questions (or approval request) to the person and waits for the
// answer (ADR 0x042). The result is JSON the model reads: the answers, the approval decision, or
// that nobody answered in time, in which case the turn is wrapped up (chatContext.awaitingUser)
// and the model is told to ask in its reply.
func (a *Agent) askUserTool(ctx context.Context, chatCtx *chatContext, input []byte) (string, error) {
	payload, err := agenttools.ParseAskUserArgs(input)
	if err != nil {
		return "", err
	}
	out, err := a.awaitPrompt(ctx, chatCtx, models.AgentPromptSourceAgent, toolUseIDFrom(ctx), payload)
	if err != nil {
		return "", err
	}
	return marshalToolJSON(askUserResult(payload, out))
}

// askUserResult is what the model reads back.
func askUserResult(payload models.AgentPromptPayload, out promptOutcome) map[string]any {
	if !out.answered() {
		return map[string]any{
			"status": "no_answer",
			"instruction": "The user did not answer in time. Do not guess and do not keep working on anything that depends on the answer: " +
				"end your turn now with a short reply that asks the question again, so they can answer when they are back.",
		}
	}
	resp := out.response
	if payload.Kind == models.AgentPromptKindApproval {
		res := map[string]any{"status": "answered", "approved": resp.Action == models.AgentPromptAccept}
		if resp.Action == models.AgentPromptCancel {
			res["status"] = "dismissed"
			res["approved"] = false
		}
		if resp.Note != "" {
			res["note"] = resp.Note
		}
		return res
	}
	if resp.Action != models.AgentPromptAccept {
		res := map[string]any{
			"status":      "declined",
			"instruction": "The user chose not to answer. Carry on without it if you can, using sensible defaults and saying which you chose; otherwise end your turn and say what you need.",
		}
		if resp.Note != "" {
			res["note"] = resp.Note
		}
		return res
	}
	answers := make([]map[string]any, 0, len(payload.Questions))
	for _, q := range payload.Questions {
		entry := map[string]any{"question": q.Question}
		ans, ok := resp.Answers[q.ID]
		switch {
		case !ok:
			entry["answer"] = nil
			entry["skipped"] = true
		default:
			parts := append([]string(nil), ans.Selected...)
			if t := strings.TrimSpace(ans.Text); t != "" {
				parts = append(parts, t)
			}
			if q.MultiSelect {
				entry["answer"] = parts
			} else {
				entry["answer"] = strings.Join(parts, "; ")
			}
			if strings.TrimSpace(ans.Text) != "" && len(ans.Selected) == 0 && len(q.Options) > 0 {
				entry["own_answer"] = true
			}
		}
		answers = append(answers, entry)
	}
	res := map[string]any{"status": "answered", "answers": answers}
	if resp.Note != "" {
		res["note"] = resp.Note
	}
	return res
}

// marshalToolJSON encodes a tool result without HTML escaping, so text the model may copy (links
// with &, comparisons with < or >) reaches it as written.
func marshalToolJSON(v any) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}
