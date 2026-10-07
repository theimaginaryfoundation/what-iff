package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/discordbinding"
	"github.com/theimaginaryfoundation/what-iff/ent/discordbot"
	"github.com/theimaginaryfoundation/what-iff/ent/discordmessagelink"
	"github.com/theimaginaryfoundation/what-iff/ent/discordpendingpost"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Discord relay storage (docs/adr/0x025-discord-relay.md). Methods taking a userID are owner-scoped:
// a bot, binding or chat of another account is not found. Methods without one
// are for the relay itself (the gateway and the reply hook), which act on behalf
// of the owner recorded on the bot.

var (
	ErrDiscordBotNotFound     = errors.New("discord bot not found")
	ErrDiscordBindingNotFound = errors.New("discord binding not found")
	// ErrDiscordBotExists: the persona already has a bot, or the bot is already
	// connected (by this or another account).
	ErrDiscordBotExists = errors.New("discord bot already connected")
	// ErrDiscordBindingExists: the bot already has a binding for the channel.
	ErrDiscordBindingExists = errors.New("discord channel already bound for this bot")
)

// CreateDiscordBot stores a bot for one of the user's personas, encrypting its token.
func (d *Datastore) CreateDiscordBot(ctx context.Context, userID uuid.UUID, in models.DiscordBotCreate) (*models.DiscordBot, error) {
	if err := d.requireOwnedPersonality(ctx, userID, in.PersonalityID); err != nil {
		return nil, err
	}
	enc, err := d.encryptTokenForWrite(in.Token)
	if err != nil {
		return nil, err
	}
	row, err := d.dbClient.DiscordBot.Create().
		SetOwnerID(userID).
		SetPersonalityID(in.PersonalityID).
		SetApplicationID(in.ApplicationID).
		SetBotUserID(in.BotUserID).
		SetBotUsername(in.BotUsername).
		SetToken(enc).
		SetMessageContent(in.MessageContent).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrDiscordBotExists
		}
		return nil, fmt.Errorf("create discord bot: %w", err)
	}
	return discordBotModel(row), nil
}

// ListDiscordBots returns the user's bots, oldest first.
func (d *Datastore) ListDiscordBots(ctx context.Context, userID uuid.UUID) ([]models.DiscordBot, error) {
	rows, err := d.dbClient.DiscordBot.Query().
		Where(discordbot.HasOwnerWith(user.ID(userID))).
		Order(ent.Asc(discordbot.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list discord bots: %w", err)
	}
	out := make([]models.DiscordBot, 0, len(rows))
	for _, r := range rows {
		out = append(out, *discordBotModel(r))
	}
	return out, nil
}

// GetDiscordBot returns one of the user's bots.
func (d *Datastore) GetDiscordBot(ctx context.Context, userID, id uuid.UUID) (*models.DiscordBot, error) {
	row, err := d.ownedDiscordBot(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return discordBotModel(row), nil
}

// GetDiscordBotCredentials returns one of the user's bots with its token.
func (d *Datastore) GetDiscordBotCredentials(ctx context.Context, userID, id uuid.UUID) (*models.DiscordBotCredentials, error) {
	row, err := d.ownedDiscordBot(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return d.discordCredentials(row, userID)
}

// UpdateDiscordBot changes one of the user's bots.
func (d *Datastore) UpdateDiscordBot(ctx context.Context, userID, id uuid.UUID, p models.DiscordBotPatch) (*models.DiscordBot, error) {
	if _, err := d.ownedDiscordBot(ctx, userID, id); err != nil {
		return nil, err
	}
	return d.patchDiscordBot(ctx, id, p)
}

// SetDiscordBotStatus records a bot's health (the relay's view: a rejected token,
// say). An empty lastError clears it.
func (d *Datastore) SetDiscordBotStatus(ctx context.Context, id uuid.UUID, status models.DiscordBotStatus, lastError string) error {
	_, err := d.patchDiscordBot(ctx, id, models.DiscordBotPatch{Status: &status, LastError: &lastError})
	return err
}

func (d *Datastore) patchDiscordBot(ctx context.Context, id uuid.UUID, p models.DiscordBotPatch) (*models.DiscordBot, error) {
	upd := d.dbClient.DiscordBot.UpdateOneID(id)
	if p.Token != nil {
		enc, err := d.encryptTokenForWrite(*p.Token)
		if err != nil {
			return nil, err
		}
		upd.SetToken(enc)
	}
	if p.BotUsername != nil {
		upd.SetBotUsername(*p.BotUsername)
	}
	if p.MessageContent != nil {
		upd.SetMessageContent(*p.MessageContent)
	}
	if p.Status != nil {
		upd.SetStatus(discordbot.Status(*p.Status))
	}
	if p.LastError != nil {
		if *p.LastError == "" {
			upd.ClearLastError()
		} else {
			upd.SetLastError(*p.LastError)
		}
	}
	row, err := upd.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDiscordBotNotFound
		}
		return nil, fmt.Errorf("update discord bot: %w", err)
	}
	return discordBotModel(row), nil
}

// DeleteDiscordBot removes one of the user's bots and, by cascade, its bindings.
func (d *Datastore) DeleteDiscordBot(ctx context.Context, userID, id uuid.UUID) error {
	n, err := d.dbClient.DiscordBot.Delete().
		Where(discordbot.ID(id), discordbot.HasOwnerWith(user.ID(userID))).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete discord bot: %w", err)
	}
	if n == 0 {
		return ErrDiscordBotNotFound
	}
	return nil
}

// ListActiveDiscordBotCredentials returns every active bot of every user with its
// token: the set the gateway should hold connections for. A bot whose token cannot
// be decrypted is skipped (and logged) rather than failing the whole list.
func (d *Datastore) ListActiveDiscordBotCredentials(ctx context.Context) ([]models.DiscordBotCredentials, error) {
	rows, err := d.dbClient.DiscordBot.Query().
		Where(discordbot.StatusEQ(discordbot.StatusActive)).
		WithOwner().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active discord bots: %w", err)
	}
	out := make([]models.DiscordBotCredentials, 0, len(rows))
	for _, r := range rows {
		if r.Edges.Owner == nil {
			continue
		}
		creds, err := d.discordCredentials(r, r.Edges.Owner.ID)
		if err != nil {
			d.logger.Warn("discord bot token unreadable; skipping")
			continue
		}
		out = append(out, *creds)
	}
	return out, nil
}

// CreateDiscordBinding binds a channel seen by one of the user's bots to one of
// the user's chats.
func (d *Datastore) CreateDiscordBinding(ctx context.Context, userID, botID uuid.UUID, in models.DiscordBindingCreate) (*models.DiscordBinding, error) {
	if _, err := d.ownedDiscordBot(ctx, userID, botID); err != nil {
		return nil, err
	}
	if err := d.requireOwnedChat(ctx, userID, in.ChatID); err != nil {
		return nil, err
	}
	row, err := d.dbClient.DiscordBinding.Create().
		SetBotID(botID).
		SetChatID(in.ChatID).
		SetGuildID(in.GuildID).
		SetChannelID(in.ChannelID).
		SetGuildName(in.GuildName).
		SetChannelName(in.ChannelName).
		SetInboundEnabled(in.InboundEnabled).
		SetAllowUserIds(nonNilStrings(in.AllowUserIDs)).
		SetDenyUserIds(nonNilStrings(in.DenyUserIDs)).
		SetAllowUnrestricted(in.AllowUnrestricted).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrDiscordBindingExists
		}
		return nil, fmt.Errorf("create discord binding: %w", err)
	}
	return discordBindingModel(row, botID), nil
}

// ListDiscordBindings returns all of the user's bindings, oldest first.
func (d *Datastore) ListDiscordBindings(ctx context.Context, userID uuid.UUID) ([]models.DiscordBinding, error) {
	rows, err := d.dbClient.DiscordBinding.Query().
		Where(discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID)))).
		WithBot().
		Order(ent.Asc(discordbinding.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list discord bindings: %w", err)
	}
	return bindingModels(rows), nil
}

// ListDiscordBindingsForChat returns the user's bindings whose relay thread is chatID.
func (d *Datastore) ListDiscordBindingsForChat(ctx context.Context, userID, chatID uuid.UUID) ([]models.DiscordBinding, error) {
	rows, err := d.dbClient.DiscordBinding.Query().
		Where(
			discordbinding.ChatID(chatID),
			discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID))),
		).
		WithBot().
		Order(ent.Asc(discordbinding.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list discord bindings for chat: %w", err)
	}
	return bindingModels(rows), nil
}

// ListDiscordBindingsForPersonality returns the user's bindings whose bot speaks as
// personalityID: where that persona can post.
func (d *Datastore) ListDiscordBindingsForPersonality(ctx context.Context, userID, personalityID uuid.UUID) ([]models.DiscordBinding, error) {
	rows, err := d.dbClient.DiscordBinding.Query().
		Where(discordbinding.HasBotWith(
			discordbot.PersonalityID(personalityID),
			discordbot.StatusEQ(discordbot.StatusActive),
			discordbot.HasOwnerWith(user.ID(userID)),
		)).
		WithBot().
		Order(ent.Asc(discordbinding.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list discord bindings for personality: %w", err)
	}
	return bindingModels(rows), nil
}

// GetDiscordBinding returns one of the user's bindings.
func (d *Datastore) GetDiscordBinding(ctx context.Context, userID, id uuid.UUID) (*models.DiscordBinding, error) {
	row, err := d.dbClient.DiscordBinding.Query().
		Where(discordbinding.ID(id), discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID)))).
		WithBot().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDiscordBindingNotFound
		}
		return nil, fmt.Errorf("get discord binding: %w", err)
	}
	return discordBindingModel(row, row.Edges.Bot.ID), nil
}

// UpdateDiscordBinding changes one of the user's bindings. Moving it to another
// chat checks that chat is the user's.
func (d *Datastore) UpdateDiscordBinding(ctx context.Context, userID, id uuid.UUID, p models.DiscordBindingPatch) (*models.DiscordBinding, error) {
	if _, err := d.GetDiscordBinding(ctx, userID, id); err != nil {
		return nil, err
	}
	if p.ChatID != nil {
		if err := d.requireOwnedChat(ctx, userID, *p.ChatID); err != nil {
			return nil, err
		}
	}
	return d.patchDiscordBinding(ctx, id, p)
}

// WithdrawDiscordBindingAcknowledgement clears a binding's "not sandboxed is fine" acknowledgement.
// The relay calls it when it sees the bound thread sandboxed again, so switching the sandbox off
// later cannot silently re-open the binding.
func (d *Datastore) WithdrawDiscordBindingAcknowledgement(ctx context.Context, id uuid.UUID) error {
	off := false
	_, err := d.patchDiscordBinding(ctx, id, models.DiscordBindingPatch{AllowUnrestricted: &off})
	return err
}

// SetDiscordBindingStatus records a binding's health from the relay (a channel it
// can no longer post to, say). An empty lastError clears it.
func (d *Datastore) SetDiscordBindingStatus(ctx context.Context, id uuid.UUID, status models.DiscordBindingStatus, lastError string) error {
	_, err := d.patchDiscordBinding(ctx, id, models.DiscordBindingPatch{Status: &status, LastError: &lastError})
	return err
}

// TouchDiscordBinding records activity on a binding.
func (d *Datastore) TouchDiscordBinding(ctx context.Context, id uuid.UUID) error {
	return d.dbClient.DiscordBinding.UpdateOneID(id).SetLastActivityAt(time.Now()).Exec(ctx)
}

func (d *Datastore) patchDiscordBinding(ctx context.Context, id uuid.UUID, p models.DiscordBindingPatch) (*models.DiscordBinding, error) {
	upd := d.dbClient.DiscordBinding.UpdateOneID(id)
	if p.ChatID != nil {
		upd.SetChatID(*p.ChatID)
	}
	if p.ChannelName != nil {
		upd.SetChannelName(*p.ChannelName)
	}
	if p.GuildName != nil {
		upd.SetGuildName(*p.GuildName)
	}
	if p.InboundEnabled != nil {
		upd.SetInboundEnabled(*p.InboundEnabled)
	}
	if p.AllowUserIDs != nil {
		upd.SetAllowUserIds(nonNilStrings(*p.AllowUserIDs))
	}
	if p.DenyUserIDs != nil {
		upd.SetDenyUserIds(nonNilStrings(*p.DenyUserIDs))
	}
	if p.AllowUnrestricted != nil {
		upd.SetAllowUnrestricted(*p.AllowUnrestricted)
	}
	if p.Status != nil {
		upd.SetStatus(discordbinding.Status(*p.Status))
	}
	if p.LastError != nil {
		if *p.LastError == "" {
			upd.ClearLastError()
		} else {
			upd.SetLastError(*p.LastError)
		}
	}
	row, err := upd.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDiscordBindingNotFound
		}
		return nil, fmt.Errorf("update discord binding: %w", err)
	}
	botID, err := row.QueryBot().OnlyID(ctx)
	if err != nil {
		return nil, fmt.Errorf("discord binding bot: %w", err)
	}
	return discordBindingModel(row, botID), nil
}

// DeleteDiscordBinding removes one of the user's bindings.
func (d *Datastore) DeleteDiscordBinding(ctx context.Context, userID, id uuid.UUID) error {
	n, err := d.dbClient.DiscordBinding.Delete().
		Where(discordbinding.ID(id), discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID)))).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete discord binding: %w", err)
	}
	if n == 0 {
		return ErrDiscordBindingNotFound
	}
	return nil
}

// FindDiscordBindingTarget resolves the binding a bot has for a channel, with the
// bot's credentials and owner. ErrDiscordBindingNotFound when the channel is not
// bound for that bot.
func (d *Datastore) FindDiscordBindingTarget(ctx context.Context, botID uuid.UUID, channelID string) (*models.DiscordBindingTarget, error) {
	row, err := d.dbClient.DiscordBinding.Query().
		Where(discordbinding.ChannelID(channelID), discordbinding.HasBotWith(discordbot.ID(botID))).
		WithBot(func(q *ent.DiscordBotQuery) { q.WithOwner() }).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDiscordBindingNotFound
		}
		return nil, fmt.Errorf("find discord binding: %w", err)
	}
	return d.bindingTarget(row)
}

// GetDiscordBindingTarget resolves a binding by id for the relay.
func (d *Datastore) GetDiscordBindingTarget(ctx context.Context, bindingID uuid.UUID) (*models.DiscordBindingTarget, error) {
	row, err := d.dbClient.DiscordBinding.Query().
		Where(discordbinding.ID(bindingID)).
		WithBot(func(q *ent.DiscordBotQuery) { q.WithOwner() }).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDiscordBindingNotFound
		}
		return nil, fmt.Errorf("get discord binding target: %w", err)
	}
	return d.bindingTarget(row)
}

func (d *Datastore) bindingTarget(row *ent.DiscordBinding) (*models.DiscordBindingTarget, error) {
	bot := row.Edges.Bot
	if bot == nil || bot.Edges.Owner == nil {
		return nil, ErrDiscordBindingNotFound
	}
	creds, err := d.discordCredentials(bot, bot.Edges.Owner.ID)
	if err != nil {
		return nil, err
	}
	return &models.DiscordBindingTarget{Binding: *discordBindingModel(row, bot.ID), Bot: *creds}, nil
}

// RecordInboundDiscordMessage records a Discord message that is about to start a
// turn. created is false when the message was already recorded (a redelivered
// event), in which case the caller must not start another turn.
func (d *Datastore) RecordInboundDiscordMessage(ctx context.Context, in models.DiscordInbound) (link *models.DiscordMessageLink, created bool, err error) {
	row, err := d.dbClient.DiscordMessageLink.Create().
		SetBindingID(in.BindingID).
		SetDirection(discordmessagelink.DirectionInbound).
		SetDiscordMessageID(in.DiscordMessageID).
		SetDiscordChannelID(in.DiscordChannelID).
		SetAuthorID(in.AuthorID).
		SetAuthorName(in.AuthorName).
		SetStatus(discordmessagelink.StatusReceived).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("record inbound discord message: %w", err)
	}
	return discordLinkModel(row, in.BindingID), true, nil
}

// AttachDiscordLinkMessage records the What Iff message an inbound link became.
func (d *Datastore) AttachDiscordLinkMessage(ctx context.Context, linkID, chatMessageID uuid.UUID) error {
	return d.dbClient.DiscordMessageLink.UpdateOneID(linkID).SetChatMessageID(chatMessageID).Exec(ctx)
}

// DeleteDiscordLink removes a link (an inbound one whose turn could not start, so
// a redelivery may try again).
func (d *Datastore) DeleteDiscordLink(ctx context.Context, linkID uuid.UUID) error {
	return d.dbClient.DiscordMessageLink.DeleteOneID(linkID).Exec(ctx)
}

// FindInboundDiscordLink returns the inbound link for a saved user message, if
// that message came from Discord.
func (d *Datastore) FindInboundDiscordLink(ctx context.Context, chatMessageID uuid.UUID) (*models.DiscordMessageLink, error) {
	row, err := d.dbClient.DiscordMessageLink.Query().
		Where(
			discordmessagelink.ChatMessageID(chatMessageID),
			discordmessagelink.DirectionEQ(discordmessagelink.DirectionInbound),
		).
		WithBinding().
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("find inbound discord link: %w", err)
	}
	return discordLinkModel(row, row.Edges.Binding.ID), nil
}

// StartOutboundDiscordLink records that a reply is being posted to a binding. If
// it was already recorded (a retried hook), the existing link is returned with
// created false, and the caller posts only if it is not yet sent.
func (d *Datastore) StartOutboundDiscordLink(ctx context.Context, bindingID, chatMessageID uuid.UUID, channelID string, replyTo *uuid.UUID) (*models.DiscordMessageLink, bool, error) {
	existing, err := d.dbClient.DiscordMessageLink.Query().
		Where(
			discordmessagelink.ChatMessageID(chatMessageID),
			discordmessagelink.DirectionEQ(discordmessagelink.DirectionOutbound),
			discordmessagelink.HasBindingWith(discordbinding.ID(bindingID)),
		).
		First(ctx)
	if err == nil {
		return discordLinkModel(existing, bindingID), false, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, fmt.Errorf("find outbound discord link: %w", err)
	}
	create := d.dbClient.DiscordMessageLink.Create().
		SetBindingID(bindingID).
		SetDirection(discordmessagelink.DirectionOutbound).
		SetChatMessageID(chatMessageID).
		SetDiscordChannelID(channelID).
		SetStatus(discordmessagelink.StatusPending)
	if replyTo != nil {
		create.SetReplyToLinkID(*replyTo)
	}
	row, err := create.Save(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("start outbound discord link: %w", err)
	}
	return discordLinkModel(row, bindingID), true, nil
}

// FinishOutboundDiscordLink records the outcome of posting a reply.
func (d *Datastore) FinishOutboundDiscordLink(ctx context.Context, linkID uuid.UUID, postedIDs []string, postErr error) error {
	upd := d.dbClient.DiscordMessageLink.UpdateOneID(linkID).
		AddAttempts(1).
		SetPostedMessageIds(nonNilStrings(postedIDs))
	if postErr != nil {
		upd.SetStatus(discordmessagelink.StatusFailed).SetError(postErr.Error())
	} else {
		upd.SetStatus(discordmessagelink.StatusSent).ClearError()
	}
	return upd.Exec(ctx)
}

// ListDiscordLinksForChat returns the most recent links of one of the user's chats,
// for showing "via Discord" and "Posted to #channel" in the UI.
func (d *Datastore) ListDiscordLinksForChat(ctx context.Context, userID, chatID uuid.UUID, limit int) ([]models.DiscordMessageLink, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := d.dbClient.DiscordMessageLink.Query().
		Where(discordmessagelink.HasBindingWith(
			discordbinding.ChatID(chatID),
			discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID))),
		)).
		WithBinding().
		Order(ent.Desc(discordmessagelink.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list discord links for chat: %w", err)
	}
	out := make([]models.DiscordMessageLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, *discordLinkModel(r, r.Edges.Binding.ID))
	}
	return out, nil
}

// SetDiscordPendingPost asks for the next reply in chatID to be posted to the
// user's binding (replacing an earlier request for the same pair).
func (d *Datastore) SetDiscordPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID, source models.DiscordPendingPostSource) error {
	if _, err := d.GetDiscordBinding(ctx, userID, bindingID); err != nil {
		return err
	}
	if err := d.ClearDiscordPendingPost(ctx, userID, bindingID, chatID); err != nil {
		return err
	}
	return d.dbClient.DiscordPendingPost.Create().
		SetBindingID(bindingID).
		SetChatID(chatID).
		SetSource(discordpendingpost.Source(source)).
		Exec(ctx)
}

// ClearDiscordPendingPost withdraws a request to post the next reply.
func (d *Datastore) ClearDiscordPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID) error {
	_, err := d.dbClient.DiscordPendingPost.Delete().
		Where(
			discordpendingpost.ChatID(chatID),
			discordpendingpost.HasBindingWith(
				discordbinding.ID(bindingID),
				discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID))),
			),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("clear discord pending post: %w", err)
	}
	return nil
}

// ConsumeDiscordPendingPosts removes and returns the bindings that asked for the
// next reply in chatID to be posted. Expired requests are dropped, not returned.
func (d *Datastore) ConsumeDiscordPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error) {
	owned := discordpendingpost.HasBindingWith(discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID))))
	rows, err := d.dbClient.DiscordPendingPost.Query().
		Where(discordpendingpost.ChatID(chatID), owned).
		WithBinding().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list discord pending posts: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	now := time.Now()
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	var rowIDs []uuid.UUID
	for _, r := range rows {
		rowIDs = append(rowIDs, r.ID)
		if r.ExpiresAt.Before(now) || r.Edges.Binding == nil || seen[r.Edges.Binding.ID] {
			continue
		}
		seen[r.Edges.Binding.ID] = true
		ids = append(ids, r.Edges.Binding.ID)
	}
	if _, err := d.dbClient.DiscordPendingPost.Delete().Where(discordpendingpost.IDIn(rowIDs...)).Exec(ctx); err != nil {
		return nil, fmt.Errorf("consume discord pending posts: %w", err)
	}
	return ids, nil
}

// ListDiscordPendingPosts returns which of the user's bindings are set to post the
// next reply in chatID (for showing the composer toggle's state).
func (d *Datastore) ListDiscordPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := d.dbClient.DiscordPendingPost.Query().
		Where(
			discordpendingpost.ChatID(chatID),
			discordpendingpost.ExpiresAtGT(time.Now()),
			discordpendingpost.HasBindingWith(discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID)))),
		).
		WithBinding().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list discord pending posts: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.Edges.Binding != nil {
			ids = append(ids, r.Edges.Binding.ID)
		}
	}
	return ids, nil
}

// IsExternalRelayChat reports whether chatID is one of the user's Discord relay threads: bound to
// a Discord channel by one of the user's bots, sandboxed or not. People outside the
// account speak there, so memories written from it are stored with external provenance
// (models.MemoryProvenanceExternal). The sandbox decides what such a thread may read, not who is
// talking in it, so an owner who unsandboxed a relay thread still gets its memories marked.
func (d *Datastore) IsExternalRelayChat(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
	return externalRelayChat(ctx, d.dbClient, userID, chatID)
}

// externalRelayChat is IsExternalRelayChat on any client (a transaction's included).
func externalRelayChat(ctx context.Context, client *ent.Client, userID, chatID uuid.UUID) (bool, error) {
	if chatID == uuid.Nil {
		return false, nil
	}
	return client.DiscordBinding.Query().
		Where(
			discordbinding.ChatID(chatID),
			discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID))),
		).
		Exist(ctx)
}

// ExternalSpeakerForMessage returns the Discord display name recorded for one of the user's saved
// messages, when that message arrived from Discord through the relay; "" otherwise.
func (d *Datastore) ExternalSpeakerForMessage(ctx context.Context, userID, chatMessageID uuid.UUID) (string, error) {
	if chatMessageID == uuid.Nil {
		return "", nil
	}
	row, err := d.dbClient.DiscordMessageLink.Query().
		Where(
			discordmessagelink.ChatMessageID(chatMessageID),
			discordmessagelink.DirectionEQ(discordmessagelink.DirectionInbound),
			discordmessagelink.HasBindingWith(discordbinding.HasBotWith(discordbot.HasOwnerWith(user.ID(userID)))),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("find discord speaker: %w", err)
	}
	return row.AuthorName, nil
}

// requireOwnedPersonality is ErrPersonalityNotFound unless the persona is the user's.
func (d *Datastore) requireOwnedPersonality(ctx context.Context, userID, id uuid.UUID) error {
	ok, err := d.dbClient.Personality.Query().Where(personality.ID(id), personality.HasUserWith(user.ID(userID))).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check personality: %w", err)
	}
	if !ok {
		return ErrPersonalityNotFound
	}
	return nil
}

// requireOwnedChat is ErrChatNotFound unless the chat is the user's.
func (d *Datastore) requireOwnedChat(ctx context.Context, userID, id uuid.UUID) error {
	ok, err := d.dbClient.Chat.Query().Where(chat.ID(id), chat.HasOwnerWith(user.ID(userID))).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check chat: %w", err)
	}
	if !ok {
		return ErrChatNotFound
	}
	return nil
}

func (d *Datastore) ownedDiscordBot(ctx context.Context, userID, id uuid.UUID) (*ent.DiscordBot, error) {
	row, err := d.dbClient.DiscordBot.Query().
		Where(discordbot.ID(id), discordbot.HasOwnerWith(user.ID(userID))).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDiscordBotNotFound
		}
		return nil, fmt.Errorf("get discord bot: %w", err)
	}
	return row, nil
}

func (d *Datastore) discordCredentials(row *ent.DiscordBot, ownerID uuid.UUID) (*models.DiscordBotCredentials, error) {
	token, err := d.decryptTokenForRead(row.Token)
	if err != nil {
		return nil, fmt.Errorf("decrypt discord bot token: %w", err)
	}
	return &models.DiscordBotCredentials{
		ID:             row.ID,
		OwnerID:        ownerID,
		PersonalityID:  row.PersonalityID,
		BotUserID:      row.BotUserID,
		Token:          token,
		MessageContent: row.MessageContent,
	}, nil
}

func discordBotModel(r *ent.DiscordBot) *models.DiscordBot {
	return &models.DiscordBot{
		ID:             r.ID,
		PersonalityID:  r.PersonalityID,
		ApplicationID:  r.ApplicationID,
		BotUserID:      r.BotUserID,
		BotUsername:    r.BotUsername,
		MessageContent: r.MessageContent,
		Status:         models.DiscordBotStatus(r.Status),
		LastError:      r.LastError,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func bindingModels(rows []*ent.DiscordBinding) []models.DiscordBinding {
	out := make([]models.DiscordBinding, 0, len(rows))
	for _, r := range rows {
		var botID uuid.UUID
		if r.Edges.Bot != nil {
			botID = r.Edges.Bot.ID
		}
		out = append(out, *discordBindingModel(r, botID))
	}
	return out
}

func discordBindingModel(r *ent.DiscordBinding, botID uuid.UUID) *models.DiscordBinding {
	return &models.DiscordBinding{
		ID:                r.ID,
		BotID:             botID,
		ChatID:            r.ChatID,
		GuildID:           r.GuildID,
		ChannelID:         r.ChannelID,
		GuildName:         r.GuildName,
		ChannelName:       r.ChannelName,
		InboundEnabled:    r.InboundEnabled,
		AllowUserIDs:      nonNilStrings(r.AllowUserIds),
		DenyUserIDs:       nonNilStrings(r.DenyUserIds),
		AllowUnrestricted: r.AllowUnrestricted,
		Status:            models.DiscordBindingStatus(r.Status),
		LastError:         r.LastError,
		LastActivityAt:    r.LastActivityAt,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}

func discordLinkModel(r *ent.DiscordMessageLink, bindingID uuid.UUID) *models.DiscordMessageLink {
	return &models.DiscordMessageLink{
		ID:               r.ID,
		BindingID:        bindingID,
		Direction:        string(r.Direction),
		ChatMessageID:    r.ChatMessageID,
		DiscordMessageID: r.DiscordMessageID,
		DiscordChannelID: r.DiscordChannelID,
		PostedMessageIDs: nonNilStrings(r.PostedMessageIds),
		AuthorID:         r.AuthorID,
		AuthorName:       r.AuthorName,
		ReplyToLinkID:    r.ReplyToLinkID,
		Status:           models.DiscordLinkStatus(r.Status),
		Error:            r.Error,
		Attempts:         r.Attempts,
		CreatedAt:        r.CreatedAt,
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
