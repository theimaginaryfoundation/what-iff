package agent

// post_to_discord: the Discord relay's tool. It does not send anything itself. It
// marks the reply being written for posting to one of the persona's connected
// channels, and the relay posts the finished reply as written (output mirroring:
// one reply, two destinations). See internal/discordrelay and
// docs/adr/0x025-discord-relay.md.
//
// It registers through the agent's extension seams (tools.AdditionalFunctionToolCatalog,
// extraToolHandlersForChat, additionalDisabledToolsForChat,
// additionalDeveloperContextForChat). Those are single function variables that other
// builds set as well (the private build's shell tool does), so this file wraps
// whatever is already there instead of replacing it, and anything that sets them
// after it must do the same. Go runs a package's init functions in file-name order.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const discordPostToolName = "post_to_discord"

var discordPostToolSpec = tools.FunctionToolSpec{
	Name: discordPostToolName,
	Description: "Post the reply you are writing now to a connected Discord channel. " +
		"Call it when the user asks you to post, share or send your reply to Discord. " +
		"It takes no text: after the call, finish your reply normally, and that reply is posted to the channel exactly as you write it. " +
		"Only channels listed in your instructions can be used.",
	Properties: map[string]interface{}{
		"channel": map[string]interface{}{
			"type":        "string",
			"description": "The channel, as listed in your instructions (e.g. \"#general\", or \"Server/#general\" when two servers have the same channel name).",
		},
	},
	Required: []string{"channel"},
}

// discordToolStore is the storage the tool needs. *datastore.Datastore satisfies it.
type discordToolStore interface {
	ListDiscordBindingsForPersonality(ctx context.Context, userID, personalityID uuid.UUID) ([]models.DiscordBinding, error)
	SetDiscordPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID, source models.DiscordPendingPostSource) error
}

// discordToolStoreFor is how the tool reaches storage; tests replace it.
var discordToolStoreFor = func(a *Agent) discordToolStore {
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
			Spec:             discordPostToolSpec,
			HumanDescription: "Post a reply to one of this personality's connected Discord channels.",
			UserGuide:        "When this personality has a Discord bot, ask it to post its reply to a channel, e.g. \"post that to #general\". The reply is posted exactly as it appears here.",
			AgentDefault:     true,
			// Offered in a sandbox: discordChannelsFor lets a sandboxed (or bound) chat reach only
			// its own channel, so the tool cannot post anywhere a stranger should not reach.
			SandboxPolicy: tools.SandboxAllowed,
			// Not toggleable: the Tools tab lists toggleable tools for every thread,
			// and this one only means something where the persona has a Discord bot.
			// It is disabled per chat instead when the persona has no connected
			// channel (additionalDisabledToolsForChat).
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
		handlers[discordPostToolName] = func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := discordPost(ctx, discordToolStoreFor(a), chat, input)
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
		if len(discordChannelsFor(context.Background(), discordToolStoreFor(a), chat)) == 0 {
			disabled[discordPostToolName] = true
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
		if s := discordDeveloperContext(discordChannelsFor(context.Background(), discordToolStoreFor(a), chat), chat, discordToolAvailable(chat)); s != "" {
			parts = append(parts, s)
		}
		return strings.Join(parts, "\n\n")
	}
}

// discordChannelsFor lists the channels the chat can post to: none without a
// persona, or an active bot with bindings. An ordinary chat (the owner's own
// conversation) may reach every channel its persona's bot is bound to. A SANDBOXED
// chat (which every relay thread is by default) is a sandbox for whoever can tag
// the bot, so it may reach only the channel(s) bound to this very thread: it must not learn the names of, or
// post into, the owner's other servers and channels.
func discordChannelsFor(ctx context.Context, store discordToolStore, chat *models.Chat) []models.DiscordBinding {
	if store == nil || chat == nil || chat.PersonalityID == uuid.Nil {
		return nil
	}
	bindings, err := store.ListDiscordBindingsForPersonality(ctx, chat.UserID, chat.PersonalityID)
	if err != nil {
		return nil
	}
	// A relay thread (any chat a binding points at) is driven by people outside the account
	// too, so, like a sandboxed chat, it reaches only its own channel(s).
	ownOnly := chat.IsSandboxed()
	for _, b := range bindings {
		if b.ChatID == chat.ID {
			ownOnly = true
		}
	}
	var usable []models.DiscordBinding
	for _, b := range bindings {
		if b.Status != models.DiscordBindingActive {
			continue
		}
		if ownOnly && b.ChatID != chat.ID {
			continue
		}
		usable = append(usable, b)
	}
	return usable
}

// discordToolAvailable reports whether the chat's tool settings leave post_to_discord on.
func discordToolAvailable(chat *models.Chat) bool {
	return chat != nil && chat.ToolsEnabled && !slices.Contains(chat.DisabledTools, discordPostToolName)
}

func channelLabel(b models.DiscordBinding, qualified bool) string {
	name := "#" + strings.TrimPrefix(b.ChannelName, "#")
	if b.ChannelName == "" {
		name = "#" + b.ChannelID
	}
	if qualified && b.GuildName != "" {
		return b.GuildName + "/" + name
	}
	return name
}

// labels gives each binding the shortest unambiguous label.
func labels(bindings []models.DiscordBinding) []string {
	counts := map[string]int{}
	for _, b := range bindings {
		counts[strings.ToLower(channelLabel(b, false))]++
	}
	out := make([]string, len(bindings))
	for i, b := range bindings {
		out[i] = channelLabel(b, counts[strings.ToLower(channelLabel(b, false))] > 1)
	}
	return out
}

// discordDeveloperContext tells the model where it can post (only when the tool is available
// to it) and, in a relay thread, how Discord messages appear there.
func discordDeveloperContext(bindings []models.DiscordBinding, chat *models.Chat, toolAvailable bool) string {
	if len(bindings) == 0 {
		return ""
	}
	ls := labels(bindings)
	var b strings.Builder
	b.WriteString("You are also present on Discord as your own bot.")
	if toolAvailable {
		b.WriteString(" Channels you can post to: ")
		for i, l := range ls {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(l)
			if bindings[i].GuildName != "" && !strings.Contains(l, "/") {
				fmt.Fprintf(&b, " (in %s)", bindings[i].GuildName)
			}
		}
		b.WriteString(". To post your reply to one, call post_to_discord with the channel and then write the reply as you want it to appear there.")
	}
	for _, bd := range bindings {
		if chat != nil && bd.ChatID == chat.ID {
			b.WriteString(" This thread is the relay for " + channelLabel(bd, false) + ": messages from Discord appear here as \"name (Discord, #channel): text\", and your replies to them are posted back to Discord automatically, where other people can read them.")
			break
		}
	}
	return b.String()
}

func discordPost(ctx context.Context, store discordToolStore, chat *models.Chat, input []byte) (string, error) {
	var args struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(input, &args); err != nil || strings.TrimSpace(args.Channel) == "" {
		return "", fmt.Errorf("post_to_discord needs a channel")
	}
	bindings := discordChannelsFor(ctx, store, chat)
	if len(bindings) == 0 {
		return "", fmt.Errorf("this persona has no connected Discord channels")
	}
	ls := labels(bindings)
	want := normalizeChannel(args.Channel)
	var match []int
	for i, b := range bindings {
		if normalizeChannel(ls[i]) == want || normalizeChannel(channelLabel(b, true)) == want || b.ChannelID == strings.TrimSpace(args.Channel) {
			match = append(match, i)
		}
	}
	if len(match) == 0 {
		for i, b := range bindings {
			if normalizeChannel(channelLabel(b, false)) == want {
				match = append(match, i)
			}
		}
	}
	switch len(match) {
	case 0:
		if chat.IsSandboxed() {
			return "", fmt.Errorf("this is a sandboxed thread, so it can only post to its own Discord channel (%s); %q is not available", strings.Join(ls, ", "), args.Channel)
		}
		return "", fmt.Errorf("unknown channel %q; use one of: %s", args.Channel, strings.Join(ls, ", "))
	case 1:
	default:
		return "", fmt.Errorf("%q matches more than one channel; use one of: %s", args.Channel, strings.Join(ls, ", "))
	}
	b := bindings[match[0]]
	if err := store.SetDiscordPendingPost(ctx, chat.UserID, b.ID, chat.ID, models.DiscordPostFromTool); err != nil {
		return "", fmt.Errorf("could not schedule the post: %w", err)
	}
	return fmt.Sprintf("Your reply will be posted to %s when you finish it. Write it now, as it should appear there.", ls[match[0]]), nil
}

func normalizeChannel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[:i+1] + strings.TrimPrefix(s[i+1:], "#")
	}
	return strings.TrimPrefix(s, "#")
}
