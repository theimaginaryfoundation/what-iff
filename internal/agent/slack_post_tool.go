package agent

// post_to_slack: the Slack relay's tool, the twin of post_to_discord and post_to_buzz. It does
// not send anything itself: it marks the reply being written for posting to one of the persona's
// Slack channels, and the relay posts the finished reply as written. See internal/slackrelay and
// docs/adr/0x032-slack-relay.md.
//
// It registers through the same extension seams as post_to_discord, wrapping whatever is
// already there (the files' inits may run in any order).

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/slackrelay"
)

const slackPostToolName = "post_to_slack"

var slackPostToolSpec = tools.FunctionToolSpec{
	Name: slackPostToolName,
	Description: "Post the reply you are writing now to a connected Slack channel. " +
		"Call it when the user asks you to post, share or send your reply to Slack. " +
		"It takes no text: after the call, finish your reply normally, and that reply is posted to the channel exactly as you write it. " +
		"Only channels listed in your instructions can be used.",
	Properties: map[string]interface{}{
		"channel": map[string]interface{}{
			"type":        "string",
			"description": "The channel, as listed in your instructions (e.g. \"#general\", or \"Workspace/#general\" when two workspaces have the same channel name).",
		},
	},
	Required: []string{"channel"},
}

// slackToolStore is the storage the tool needs. *datastore.Datastore satisfies it.
type slackToolStore interface {
	ListSlackBindingsForPersonality(ctx context.Context, userID, personalityID uuid.UUID) ([]models.SlackBinding, error)
	SetSlackPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID, source models.SlackPendingPostSource) error
}

// slackMayUse is whether the owner may use the (experimental) relay at all
// (slackrelay.UserMayUse); tests replace it.
var slackMayUse = slackrelay.UserMayUse

// slackToolStoreFor is how the tool reaches storage; tests replace it.
var slackToolStoreFor = func(a *Agent) slackToolStore {
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
		return append(defs, tools.FunctionToolDefinition{
			Spec:             slackPostToolSpec,
			HumanDescription: "Post a reply to one of this personality's connected Slack channels.",
			UserGuide:        "When this personality is connected to Slack, ask it to post its reply to a channel, e.g. \"post that to #general on Slack\". The reply is posted exactly as it appears here.",
			AgentDefault:     true,
			// Offered in a sandbox: slackChannelsFor lets a sandboxed (or bound) chat reach only
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
		handlers[slackPostToolName] = func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := slackPost(ctx, slackToolStoreFor(a), chat, input)
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
		if len(slackChannelsFor(context.Background(), slackToolStoreFor(a), chat)) == 0 {
			disabled[slackPostToolName] = true
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
		if s := slackDeveloperContext(slackChannelsFor(context.Background(), slackToolStoreFor(a), chat), chat, slackToolAvailable(chat)); s != "" {
			parts = append(parts, s)
		}
		return strings.Join(parts, "\n\n")
	}
}

// slackChannelsFor lists the Slack channels the chat can post to. As for Discord, an ordinary
// chat may reach every channel its persona is bound to; a sandboxed chat, or a relay thread,
// only the channel(s) bound to this very thread.
func slackChannelsFor(ctx context.Context, store slackToolStore, chat *models.Chat) []models.SlackBinding {
	if store == nil || chat == nil || chat.PersonalityID == uuid.Nil || !slackMayUse(ctx, chat.UserID) {
		return nil
	}
	bindings, err := store.ListSlackBindingsForPersonality(ctx, chat.UserID, chat.PersonalityID)
	if err != nil {
		return nil
	}
	ownOnly := chat.IsSandboxed()
	for _, b := range bindings {
		if b.ChatID == chat.ID {
			ownOnly = true
		}
	}
	var usable []models.SlackBinding
	for _, b := range bindings {
		if b.Status != models.SlackBindingActive || (ownOnly && b.ChatID != chat.ID) {
			continue
		}
		usable = append(usable, b)
	}
	return usable
}

func slackToolAvailable(chat *models.Chat) bool {
	return chat != nil && chat.ToolsEnabled && !slices.Contains(chat.DisabledTools, slackPostToolName)
}

func slackChannelLabel(b models.SlackBinding, qualified bool) string {
	name := "#" + strings.TrimPrefix(b.ChannelName, "#")
	if b.ChannelName == "" {
		name = "#" + b.ChannelID
	}
	if qualified && b.TeamName != "" {
		return b.TeamName + "/" + name
	}
	return name
}

// slackLabels gives each binding the shortest unambiguous label.
func slackLabels(bindings []models.SlackBinding) []string {
	counts := map[string]int{}
	for _, b := range bindings {
		counts[strings.ToLower(slackChannelLabel(b, false))]++
	}
	out := make([]string, len(bindings))
	for i, b := range bindings {
		out[i] = slackChannelLabel(b, counts[strings.ToLower(slackChannelLabel(b, false))] > 1)
	}
	return out
}

// slackDeveloperContext tells the model where it can post (only when the tool is available) and,
// in a relay thread, how Slack messages appear there.
func slackDeveloperContext(bindings []models.SlackBinding, chat *models.Chat, toolAvailable bool) string {
	if len(bindings) == 0 {
		return ""
	}
	ls := slackLabels(bindings)
	var b strings.Builder
	b.WriteString("You are also a member of Slack workspaces, where people talk in channels.")
	if toolAvailable {
		b.WriteString(" Slack channels you can post to: ")
		for i, l := range ls {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(l)
			if bindings[i].TeamName != "" && !strings.Contains(l, "/") {
				fmt.Fprintf(&b, " (in %s)", bindings[i].TeamName)
			}
		}
		b.WriteString(". To post your reply to one, call post_to_slack with the channel and then write the reply as you want it to appear there.")
	}
	for _, bd := range bindings {
		if chat != nil && bd.ChatID == chat.ID {
			b.WriteString(" This thread is the relay for " + slackChannelLabel(bd, false) + " on Slack: messages from Slack appear here as \"name (Slack, #channel): text\", and your replies to them are posted back into that Slack thread automatically, where other people can read them. Write Markdown as usual: it is converted for Slack, and tables are shown as code blocks.")
			break
		}
	}
	return b.String()
}

func slackPost(ctx context.Context, store slackToolStore, chat *models.Chat, input []byte) (string, error) {
	var args struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(input, &args); err != nil || strings.TrimSpace(args.Channel) == "" {
		return "", fmt.Errorf("post_to_slack needs a channel")
	}
	bindings := slackChannelsFor(ctx, store, chat)
	if len(bindings) == 0 {
		return "", fmt.Errorf("this persona has no connected Slack channels")
	}
	ls := slackLabels(bindings)
	want := normalizeChannel(args.Channel)
	var match []int
	for i, b := range bindings {
		if normalizeChannel(ls[i]) == want || normalizeChannel(slackChannelLabel(b, true)) == want || b.ChannelID == strings.TrimSpace(args.Channel) {
			match = append(match, i)
		}
	}
	if len(match) == 0 {
		for i, b := range bindings {
			if normalizeChannel(slackChannelLabel(b, false)) == want {
				match = append(match, i)
			}
		}
	}
	switch len(match) {
	case 0:
		if chat.IsSandboxed() {
			return "", fmt.Errorf("this is a sandboxed thread, so it can only post to its own Slack channel (%s); %q is not available", strings.Join(ls, ", "), args.Channel)
		}
		return "", fmt.Errorf("unknown channel %q; use one of: %s", args.Channel, strings.Join(ls, ", "))
	case 1:
	default:
		return "", fmt.Errorf("%q matches more than one channel; use one of: %s", args.Channel, strings.Join(ls, ", "))
	}
	b := bindings[match[0]]
	if err := store.SetSlackPendingPost(ctx, chat.UserID, b.ID, chat.ID, models.SlackPostFromTool); err != nil {
		return "", fmt.Errorf("could not schedule the post: %w", err)
	}
	return fmt.Sprintf("Your reply will be posted to %s on Slack when you finish it. Write it now, as it should appear there.", ls[match[0]]), nil
}
