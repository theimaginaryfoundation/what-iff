package agent

// post_to_buzz: the Buzz relay's tool, the twin of post_to_discord. It does not send anything
// itself: it marks the reply being written for posting to one of the persona's Buzz channels,
// and the relay publishes the finished reply as written. See internal/buzzrelay and
// docs/adr/0x028-buzz-relay.md.
//
// It registers through the same extension seams as post_to_discord, wrapping whatever is
// already there (the two files' inits may run in either order).

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
)

const buzzPostToolName = "post_to_buzz"

var buzzPostToolSpec = tools.FunctionToolSpec{
	Name: buzzPostToolName,
	Description: "Post the reply you are writing now to a connected Buzz channel. " +
		"Call it when the user asks you to post, share or send your reply to Buzz. " +
		"It takes no text: after the call, finish your reply normally, and that reply is posted to the channel exactly as you write it. " +
		"Only channels listed in your instructions can be used.",
	Properties: map[string]interface{}{
		"channel": map[string]interface{}{
			"type":        "string",
			"description": "The channel, as listed in your instructions (e.g. \"#general\", or \"Community/#general\" when two communities have the same channel name).",
		},
	},
	Required: []string{"channel"},
}

// buzzToolStore is the storage the tool needs. *datastore.Datastore satisfies it.
type buzzToolStore interface {
	ListBuzzBindingsForPersonality(ctx context.Context, userID, personalityID uuid.UUID) ([]models.BuzzBinding, error)
	SetBuzzPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID, source models.BuzzPendingPostSource) error
	IsExternalRelayChat(ctx context.Context, userID, chatID uuid.UUID) (bool, error)
}

// buzzRelayOn is whether the operator switched the relay on (buzzrelay.Enabled); tests
// replace it.
var buzzRelayOn = buzzrelay.Enabled

// buzzToolStoreFor is how the tool reaches storage; tests replace it.
var buzzToolStoreFor = func(a *Agent) buzzToolStore {
	if a == nil || a.ds == nil {
		return nil
	}
	return a.ds
}

func init() {
	prevCatalog := tools.AdditionalFunctionToolCatalog
	tools.AdditionalFunctionToolCatalog = func() []tools.FunctionToolDefinition {
		var defs []tools.FunctionToolDefinition
		if prevCatalog != nil {
			defs = prevCatalog()
		}
		// With the relay off the tool is not listed at all (the tool picker, the onboarding
		// prompt), so a server that never turned Buzz on never mentions it.
		if !buzzRelayOn() {
			return defs
		}
		return append(defs, tools.FunctionToolDefinition{
			Spec:             buzzPostToolSpec,
			HumanDescription: "Post a reply to one of this personality's connected Buzz channels.",
			UserGuide:        "When this personality is connected to Buzz, ask it to post its reply to a channel, e.g. \"post that to #general on Buzz\". The reply is posted exactly as it appears here.",
			AgentDefault:     true,
			// Offered in a sandbox: buzzChannelsFor lets a sandboxed (or bound) chat reach only
			// its own channel.
			SandboxPolicy:  tools.SandboxAllowed,
			UserToggleable: false,
		})
	}

	prevHandlers := extraToolHandlersForChat
	extraToolHandlersForChat = func(a *Agent, chat *models.Chat) map[string]ExtraToolHandler {
		handlers := map[string]ExtraToolHandler{}
		if prevHandlers != nil {
			for k, v := range prevHandlers(a, chat) {
				handlers[k] = v
			}
		}
		handlers[buzzPostToolName] = func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := buzzPost(ctx, buzzToolStoreFor(a), chat, input)
			return out, nil, err
		}
		return handlers
	}

	prevDisabled := additionalDisabledToolsForChat
	additionalDisabledToolsForChat = func(a *Agent, chat *models.Chat) map[string]bool {
		disabled := map[string]bool{}
		if prevDisabled != nil {
			for k, v := range prevDisabled(a, chat) {
				disabled[k] = v
			}
		}
		if len(buzzChannelsFor(context.Background(), buzzToolStoreFor(a), chat)) == 0 {
			disabled[buzzPostToolName] = true
		}
		return disabled
	}

	prevContext := additionalDeveloperContextForChat
	additionalDeveloperContextForChat = func(a *Agent, chat *models.Chat) string {
		var parts []string
		if prevContext != nil {
			if s := prevContext(a, chat); s != "" {
				parts = append(parts, s)
			}
		}
		if s := buzzDeveloperContext(buzzChannelsFor(context.Background(), buzzToolStoreFor(a), chat), chat, buzzToolAvailable(chat)); s != "" {
			parts = append(parts, s)
		}
		return strings.Join(parts, "\n\n")
	}
}

// buzzChannelsFor lists the Buzz channels the chat can post to. As for Discord, an ordinary
// chat may reach every channel its persona is bound to; a sandboxed chat, or a relay thread of
// either relay, only the channel(s) bound to this very thread.
func buzzChannelsFor(ctx context.Context, store buzzToolStore, chat *models.Chat) []models.BuzzBinding {
	if store == nil || chat == nil || chat.PersonalityID == uuid.Nil || !buzzRelayOn() {
		return nil
	}
	bindings, err := store.ListBuzzBindingsForPersonality(ctx, chat.UserID, chat.PersonalityID)
	if err != nil {
		return nil
	}
	ownOnly := relayScoped(ctx, store, chat)
	var usable []models.BuzzBinding
	for _, b := range bindings {
		if b.Status != models.BuzzBindingActive || (ownOnly && b.ChatID != chat.ID) {
			continue
		}
		usable = append(usable, b)
	}
	return usable
}

func buzzToolAvailable(chat *models.Chat) bool {
	return chat != nil && chat.ToolsEnabled && !slices.Contains(chat.DisabledTools, buzzPostToolName)
}

func buzzChannelLabel(b models.BuzzBinding, qualified bool) string {
	name := "#" + strings.TrimPrefix(b.ChannelName, "#")
	if b.ChannelName == "" {
		name = "#" + b.ChannelID
	}
	if qualified && b.CommunityName != "" {
		return b.CommunityName + "/" + name
	}
	return name
}

// buzzLabels gives each binding the shortest unambiguous label.
func buzzLabels(bindings []models.BuzzBinding) []string {
	counts := map[string]int{}
	for _, b := range bindings {
		counts[strings.ToLower(buzzChannelLabel(b, false))]++
	}
	out := make([]string, len(bindings))
	for i, b := range bindings {
		out[i] = buzzChannelLabel(b, counts[strings.ToLower(buzzChannelLabel(b, false))] > 1)
	}
	return out
}

// buzzDeveloperContext tells the model where it can post (only when the tool is available) and,
// in a relay thread, how Buzz messages appear there.
func buzzDeveloperContext(bindings []models.BuzzBinding, chat *models.Chat, toolAvailable bool) string {
	if len(bindings) == 0 {
		return ""
	}
	ls := buzzLabels(bindings)
	var b strings.Builder
	b.WriteString("You are also a member of Buzz communities, where people and other agents talk in channels.")
	if toolAvailable {
		b.WriteString(" Buzz channels you can post to: ")
		for i, l := range ls {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(l)
			if bindings[i].CommunityName != "" && !strings.Contains(l, "/") {
				fmt.Fprintf(&b, " (in %s)", bindings[i].CommunityName)
			}
		}
		b.WriteString(". To post your reply to one, call post_to_buzz with the channel and then write the reply as you want it to appear there.")
	}
	for _, bd := range bindings {
		if chat != nil && bd.ChatID == chat.ID {
			b.WriteString(" This thread is the relay for " + buzzChannelLabel(bd, false) + " on Buzz: messages from Buzz appear here as \"name (Buzz, #channel): text\", and your replies to them are posted back into that conversation automatically, where other people can read them. Buzz shows Markdown." + relaykit.ContinuationNote)
			break
		}
	}
	return b.String()
}

func buzzPost(ctx context.Context, store buzzToolStore, chat *models.Chat, input []byte) (string, error) {
	var args struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(input, &args); err != nil || strings.TrimSpace(args.Channel) == "" {
		return "", fmt.Errorf("post_to_buzz needs a channel")
	}
	bindings := buzzChannelsFor(ctx, store, chat)
	if len(bindings) == 0 {
		return "", fmt.Errorf("this persona has no connected Buzz channels")
	}
	ls := buzzLabels(bindings)
	want := normalizeChannel(args.Channel)
	var match []int
	for i, b := range bindings {
		if normalizeChannel(ls[i]) == want || normalizeChannel(buzzChannelLabel(b, true)) == want || b.ChannelID == strings.TrimSpace(args.Channel) {
			match = append(match, i)
		}
	}
	if len(match) == 0 {
		for i, b := range bindings {
			if normalizeChannel(buzzChannelLabel(b, false)) == want {
				match = append(match, i)
			}
		}
	}
	switch len(match) {
	case 0:
		if chat.IsSandboxed() {
			return "", fmt.Errorf("this is a sandboxed thread, so it can only post to its own Buzz channel (%s); %q is not available", strings.Join(ls, ", "), args.Channel)
		}
		return "", fmt.Errorf("unknown channel %q; use one of: %s", args.Channel, strings.Join(ls, ", "))
	case 1:
	default:
		return "", fmt.Errorf("%q matches more than one channel; use one of: %s", args.Channel, strings.Join(ls, ", "))
	}
	b := bindings[match[0]]
	if err := store.SetBuzzPendingPost(ctx, chat.UserID, b.ID, chat.ID, models.BuzzPostFromTool); err != nil {
		return "", fmt.Errorf("could not schedule the post: %w", err)
	}
	return fmt.Sprintf("Your reply will be posted to %s on Buzz when you finish it. Write it now, as it should appear there.", ls[match[0]]), nil
}
