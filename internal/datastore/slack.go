package datastore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/slackapp"
	"github.com/theimaginaryfoundation/what-iff/ent/slackbinding"
	"github.com/theimaginaryfoundation/what-iff/ent/slackmessagelink"
	"github.com/theimaginaryfoundation/what-iff/ent/slackpendingpost"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Slack relay storage (docs/adr/0x032-slack-relay.md). As for Discord and Buzz, methods taking
// a userID are owner-scoped: another account's app, binding or chat is not found. Methods
// without one are for the relay itself (the gateway and the reply hook), which act on behalf of
// the owner recorded on the app.

var (
	ErrSlackAppNotFound     = errors.New("slack app not found")
	ErrSlackBindingNotFound = errors.New("slack binding not found")
	// ErrSlackAppExists: the Slack app is already connected (by this or another account), or
	// the persona already has an app in that workspace.
	ErrSlackAppExists = errors.New("slack app already connected")
	// ErrSlackBindingExists: the app already has a binding for the channel.
	ErrSlackBindingExists = errors.New("slack channel already bound for this app")
	// ErrSlackBindingPaused: the binding is broken, or its app is paused or its tokens refused,
	// so nothing may be posted through it.
	ErrSlackBindingPaused = errors.New("slack binding is paused or broken")
)

func slackAppOwnedBy(userID uuid.UUID) predicate.SlackApp {
	return slackapp.HasOwnerWith(user.ID(userID))
}

func slackBindingOwnedBy(userID uuid.UUID) predicate.SlackBinding {
	return slackbinding.HasAppWith(slackAppOwnedBy(userID))
}

// --- apps ---

// CreateSlackApp stores a Slack app for one of the user's personas, encrypting its tokens.
func (d *Datastore) CreateSlackApp(ctx context.Context, userID uuid.UUID, in models.SlackAppCreate) (*models.SlackApp, error) {
	if err := d.requireOwnedPersonality(ctx, userID, in.PersonalityID); err != nil {
		return nil, err
	}
	bot, err := d.encryptTokenForWrite(in.BotToken)
	if err != nil {
		return nil, err
	}
	app, err := d.encryptTokenForWrite(in.AppToken)
	if err != nil {
		return nil, err
	}
	row, err := d.dbClient.SlackApp.Create().
		SetOwnerID(userID).
		SetPersonalityID(in.PersonalityID).
		SetTeamID(in.TeamID).
		SetTeamName(in.TeamName).
		SetTeamURL(in.TeamURL).
		SetAppID(in.AppID).
		SetBotUserID(in.BotUserID).
		SetBotID(in.BotID).
		SetBotName(in.BotName).
		SetBotToken(bot).
		SetAppToken(app).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrSlackAppExists
		}
		return nil, fmt.Errorf("create slack app: %w", err)
	}
	return slackAppModel(row), nil
}

// ListSlackApps returns the user's Slack apps, oldest first.
func (d *Datastore) ListSlackApps(ctx context.Context, userID uuid.UUID) ([]models.SlackApp, error) {
	rows, err := d.dbClient.SlackApp.Query().
		Where(slackAppOwnedBy(userID)).
		Order(ent.Asc(slackapp.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list slack apps: %w", err)
	}
	out := make([]models.SlackApp, 0, len(rows))
	for _, r := range rows {
		out = append(out, *slackAppModel(r))
	}
	return out, nil
}

// GetSlackApp returns one of the user's Slack apps.
func (d *Datastore) GetSlackApp(ctx context.Context, userID, id uuid.UUID) (*models.SlackApp, error) {
	row, err := d.ownedSlackApp(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return slackAppModel(row), nil
}

// GetSlackAppCredentials returns one of the user's Slack apps with its tokens.
func (d *Datastore) GetSlackAppCredentials(ctx context.Context, userID, id uuid.UUID) (*models.SlackAppCredentials, error) {
	row, err := d.ownedSlackApp(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return d.slackCredentials(row, userID)
}

// UpdateSlackApp changes one of the user's Slack apps.
func (d *Datastore) UpdateSlackApp(ctx context.Context, userID, id uuid.UUID, p models.SlackAppPatch) (*models.SlackApp, error) {
	if _, err := d.ownedSlackApp(ctx, userID, id); err != nil {
		return nil, err
	}
	return d.patchSlackApp(ctx, id, p)
}

// SetSlackAppState records how an app's connection stands (the gateway's view). An empty
// lastError clears it; an active state without an error records the connection time (active
// with an error is a connection that is retrying). An app the owner paused is left paused, so
// a connection closing just after the pause cannot undo it. An app marked invalid_token is
// left so too: a socket that still connects (app-level tokens outlive an uninstall or a revoked
// bot token) must not make it look healthy again; only new tokens or a refresh clear it
// (UpdateSlackApp).
func (d *Datastore) SetSlackAppState(ctx context.Context, id uuid.UUID, status models.SlackAppStatus, lastError string) error {
	where := []predicate.SlackApp{slackapp.ID(id), slackapp.StatusNEQ(slackapp.StatusDisabled)}
	if status == models.SlackAppActive {
		where = append(where, slackapp.StatusNEQ(slackapp.StatusInvalidToken))
	}
	upd := d.dbClient.SlackApp.Update().
		Where(where...).
		SetStatus(slackapp.Status(status))
	if lastError == "" {
		upd.ClearLastError()
	} else {
		upd.SetLastError(lastError)
	}
	if status == models.SlackAppActive && lastError == "" {
		upd.SetLastConnectedAt(time.Now())
	}
	if _, err := upd.Save(ctx); err != nil {
		return fmt.Errorf("set slack app state: %w", err)
	}
	return nil
}

func (d *Datastore) patchSlackApp(ctx context.Context, id uuid.UUID, p models.SlackAppPatch) (*models.SlackApp, error) {
	upd := d.dbClient.SlackApp.UpdateOneID(id)
	if p.BotToken != nil {
		enc, err := d.encryptTokenForWrite(*p.BotToken)
		if err != nil {
			return nil, err
		}
		upd.SetBotToken(enc)
	}
	if p.AppToken != nil {
		enc, err := d.encryptTokenForWrite(*p.AppToken)
		if err != nil {
			return nil, err
		}
		upd.SetAppToken(enc)
	}
	if p.TeamName != nil {
		upd.SetTeamName(*p.TeamName)
	}
	if p.TeamURL != nil {
		upd.SetTeamURL(*p.TeamURL)
	}
	if p.BotName != nil {
		upd.SetBotName(*p.BotName)
	}
	if p.Status != nil {
		upd.SetStatus(slackapp.Status(*p.Status))
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
			return nil, ErrSlackAppNotFound
		}
		return nil, fmt.Errorf("update slack app: %w", err)
	}
	return slackAppModel(row), nil
}

// DeleteSlackApp removes one of the user's Slack apps and, by cascade, its bindings and links.
// Nothing is changed in Slack: the owner removes the app there.
func (d *Datastore) DeleteSlackApp(ctx context.Context, userID, id uuid.UUID) error {
	n, err := d.dbClient.SlackApp.Delete().Where(slackapp.ID(id), slackAppOwnedBy(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete slack app: %w", err)
	}
	if n == 0 {
		return ErrSlackAppNotFound
	}
	return nil
}

// ListActiveSlackAppCredentials returns every active app of every user with its tokens: the
// set the gateway should hold Socket Mode connections for. An app whose tokens cannot be
// decrypted is skipped (and logged) rather than failing the whole list.
func (d *Datastore) ListActiveSlackAppCredentials(ctx context.Context) ([]models.SlackAppCredentials, error) {
	rows, err := d.dbClient.SlackApp.Query().
		Where(slackapp.StatusEQ(slackapp.StatusActive)).
		WithOwner().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active slack apps: %w", err)
	}
	out := make([]models.SlackAppCredentials, 0, len(rows))
	for _, r := range rows {
		if r.Edges.Owner == nil {
			continue
		}
		creds, err := d.slackCredentials(r, r.Edges.Owner.ID)
		if err != nil {
			d.logger.Warn("slack app tokens unreadable; skipping")
			continue
		}
		out = append(out, *creds)
	}
	return out, nil
}

// --- bindings ---

// CreateSlackBinding binds a channel of one of the user's apps' workspaces to one of the
// user's chats.
func (d *Datastore) CreateSlackBinding(ctx context.Context, userID, appID uuid.UUID, in models.SlackBindingCreate) (*models.SlackBinding, error) {
	if _, err := d.ownedSlackApp(ctx, userID, appID); err != nil {
		return nil, err
	}
	if err := d.requireOwnedChat(ctx, userID, in.ChatID); err != nil {
		return nil, err
	}
	row, err := d.dbClient.SlackBinding.Create().
		SetAppID(appID).
		SetChatID(in.ChatID).
		SetChannelID(in.ChannelID).
		SetChannelName(in.ChannelName).
		SetIsPrivate(in.IsPrivate).
		SetInboundEnabled(in.InboundEnabled).
		SetAllowUserIds(nonNilStrings(in.AllowUserIDs)).
		SetDenyUserIds(nonNilStrings(in.DenyUserIDs)).
		SetAnswerAgents(in.AnswerAgents).
		SetAllowUnrestricted(in.AllowUnrestricted).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrSlackBindingExists
		}
		return nil, fmt.Errorf("create slack binding: %w", err)
	}
	return slackBindingModel(row, appID), nil
}

// ListSlackBindings returns all of the user's bindings, oldest first.
func (d *Datastore) ListSlackBindings(ctx context.Context, userID uuid.UUID) ([]models.SlackBinding, error) {
	return d.listSlackBindings(ctx, slackBindingOwnedBy(userID))
}

// ListSlackBindingsForChat returns the user's bindings whose relay thread is chatID.
func (d *Datastore) ListSlackBindingsForChat(ctx context.Context, userID, chatID uuid.UUID) ([]models.SlackBinding, error) {
	return d.listSlackBindings(ctx, slackBindingOwnedBy(userID), slackbinding.ChatID(chatID))
}

// ListSlackBindingsForPersonality returns the user's active bindings whose app speaks as
// personalityID and is active: where that persona can post.
func (d *Datastore) ListSlackBindingsForPersonality(ctx context.Context, userID, personalityID uuid.UUID) ([]models.SlackBinding, error) {
	return d.listSlackBindings(ctx,
		slackbinding.StatusEQ(slackbinding.StatusActive),
		slackbinding.HasAppWith(
			slackAppOwnedBy(userID),
			slackapp.PersonalityID(personalityID),
			slackapp.StatusEQ(slackapp.StatusActive),
		))
}

func (d *Datastore) listSlackBindings(ctx context.Context, where ...predicate.SlackBinding) ([]models.SlackBinding, error) {
	rows, err := d.dbClient.SlackBinding.Query().
		Where(where...).
		WithApp().
		Order(ent.Asc(slackbinding.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list slack bindings: %w", err)
	}
	out := make([]models.SlackBinding, 0, len(rows))
	for _, r := range rows {
		m := slackBindingModel(r, r.Edges.App.ID)
		m.TeamName = r.Edges.App.TeamName
		out = append(out, *m)
	}
	return out, nil
}

// GetSlackBinding returns one of the user's bindings.
func (d *Datastore) GetSlackBinding(ctx context.Context, userID, id uuid.UUID) (*models.SlackBinding, error) {
	row, err := d.dbClient.SlackBinding.Query().
		Where(slackbinding.ID(id), slackBindingOwnedBy(userID)).
		WithApp().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrSlackBindingNotFound
		}
		return nil, fmt.Errorf("get slack binding: %w", err)
	}
	m := slackBindingModel(row, row.Edges.App.ID)
	m.TeamName = row.Edges.App.TeamName
	return m, nil
}

// UpdateSlackBinding changes one of the user's bindings. Moving it to another chat checks that
// chat is the user's.
func (d *Datastore) UpdateSlackBinding(ctx context.Context, userID, id uuid.UUID, p models.SlackBindingPatch) (*models.SlackBinding, error) {
	if _, err := d.GetSlackBinding(ctx, userID, id); err != nil {
		return nil, err
	}
	if p.ChatID != nil {
		if err := d.requireOwnedChat(ctx, userID, *p.ChatID); err != nil {
			return nil, err
		}
	}
	return d.patchSlackBinding(ctx, id, p)
}

// WithdrawSlackBindingAcknowledgement clears a binding's "not sandboxed is fine"
// acknowledgement (the relay does when it sees the thread sandboxed again).
func (d *Datastore) WithdrawSlackBindingAcknowledgement(ctx context.Context, id uuid.UUID) error {
	off := false
	_, err := d.patchSlackBinding(ctx, id, models.SlackBindingPatch{AllowUnrestricted: &off})
	return err
}

// SetSlackBindingStatus records a binding's health from the relay. An empty lastError clears it.
func (d *Datastore) SetSlackBindingStatus(ctx context.Context, id uuid.UUID, status models.SlackBindingStatus, lastError string) error {
	_, err := d.patchSlackBinding(ctx, id, models.SlackBindingPatch{Status: &status, LastError: &lastError})
	return err
}

// RenameSlackChannel records a channel's current name on the app's bindings of it, when it
// changed (a channel_rename event).
func (d *Datastore) RenameSlackChannel(ctx context.Context, appID uuid.UUID, channelID, name string) error {
	name = strings.TrimPrefix(strings.TrimSpace(name), "#")
	if name == "" {
		return nil
	}
	_, err := d.dbClient.SlackBinding.Update().
		Where(
			slackbinding.ChannelID(channelID),
			slackbinding.HasAppWith(slackapp.ID(appID)),
			slackbinding.ChannelNameNEQ(name),
		).
		SetChannelName(name).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("rename slack channel: %w", err)
	}
	return nil
}

// BreakSlackChannel marks the app's bindings of a channel broken with a reason (the app was
// removed from it).
func (d *Datastore) BreakSlackChannel(ctx context.Context, appID uuid.UUID, channelID, reason string) error {
	_, err := d.dbClient.SlackBinding.Update().
		Where(slackbinding.ChannelID(channelID), slackbinding.HasAppWith(slackapp.ID(appID))).
		SetStatus(slackbinding.StatusBroken).
		SetLastError(reason).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("break slack channel: %w", err)
	}
	return nil
}

// TouchSlackBinding records activity on a binding.
func (d *Datastore) TouchSlackBinding(ctx context.Context, id uuid.UUID) error {
	return d.dbClient.SlackBinding.UpdateOneID(id).SetLastActivityAt(time.Now()).Exec(ctx)
}

func (d *Datastore) patchSlackBinding(ctx context.Context, id uuid.UUID, p models.SlackBindingPatch) (*models.SlackBinding, error) {
	upd := d.dbClient.SlackBinding.UpdateOneID(id)
	if p.ChatID != nil {
		upd.SetChatID(*p.ChatID)
	}
	if p.ChannelName != nil {
		upd.SetChannelName(*p.ChannelName)
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
	if p.AnswerAgents != nil {
		upd.SetAnswerAgents(*p.AnswerAgents)
	}
	if p.AllowUnrestricted != nil {
		upd.SetAllowUnrestricted(*p.AllowUnrestricted)
	}
	if p.Status != nil {
		upd.SetStatus(slackbinding.Status(*p.Status))
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
			return nil, ErrSlackBindingNotFound
		}
		return nil, fmt.Errorf("update slack binding: %w", err)
	}
	appID, err := row.QueryApp().OnlyID(ctx)
	if err != nil {
		return nil, fmt.Errorf("slack binding app: %w", err)
	}
	return slackBindingModel(row, appID), nil
}

// DeleteSlackBinding removes one of the user's bindings.
func (d *Datastore) DeleteSlackBinding(ctx context.Context, userID, id uuid.UUID) error {
	n, err := d.dbClient.SlackBinding.Delete().Where(slackbinding.ID(id), slackBindingOwnedBy(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete slack binding: %w", err)
	}
	if n == 0 {
		return ErrSlackBindingNotFound
	}
	return nil
}

// FindSlackBindingTarget resolves the binding an app has for a channel, with the app's
// credentials and owner. ErrSlackBindingNotFound when the channel is not bound there.
func (d *Datastore) FindSlackBindingTarget(ctx context.Context, appID uuid.UUID, channelID string) (*models.SlackBindingTarget, error) {
	return d.slackBindingTarget(ctx, slackbinding.ChannelID(channelID), slackbinding.HasAppWith(slackapp.ID(appID)))
}

// GetSlackBindingTarget resolves a binding by id for the relay.
func (d *Datastore) GetSlackBindingTarget(ctx context.Context, bindingID uuid.UUID) (*models.SlackBindingTarget, error) {
	return d.slackBindingTarget(ctx, slackbinding.ID(bindingID))
}

func (d *Datastore) slackBindingTarget(ctx context.Context, where ...predicate.SlackBinding) (*models.SlackBindingTarget, error) {
	row, err := d.dbClient.SlackBinding.Query().
		Where(where...).
		WithApp(func(q *ent.SlackAppQuery) { q.WithOwner() }).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrSlackBindingNotFound
		}
		return nil, fmt.Errorf("find slack binding: %w", err)
	}
	app := row.Edges.App
	if app == nil || app.Edges.Owner == nil {
		return nil, ErrSlackBindingNotFound
	}
	creds, err := d.slackCredentials(app, app.Edges.Owner.ID)
	if err != nil {
		return nil, err
	}
	b := slackBindingModel(row, app.ID)
	b.TeamName = app.TeamName
	return &models.SlackBindingTarget{Binding: *b, App: *creds}, nil
}

// --- message links ---

// RecordInboundSlackMessage records a message that is about to start a turn. created is false
// when it was already recorded (a redelivered event): the caller must not start another turn.
func (d *Datastore) RecordInboundSlackMessage(ctx context.Context, in models.SlackInbound) (link *models.SlackMessageLink, created bool, err error) {
	row, err := d.dbClient.SlackMessageLink.Create().
		SetBindingID(in.BindingID).
		SetDirection(slackmessagelink.DirectionInbound).
		SetMessageTs(in.MessageTS).
		SetThreadTs(in.ThreadTS).
		SetAuthorUserID(in.AuthorUserID).
		SetAuthorName(in.AuthorName).
		SetAgentDepth(in.AgentDepth).
		SetStatus(slackmessagelink.StatusReceived).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("record inbound slack message: %w", err)
	}
	return slackLinkModel(row, in.BindingID), true, nil
}

// AttachSlackLinkMessage records the What Iff message an inbound link became.
func (d *Datastore) AttachSlackLinkMessage(ctx context.Context, linkID, chatMessageID uuid.UUID) error {
	return d.dbClient.SlackMessageLink.UpdateOneID(linkID).SetChatMessageID(chatMessageID).Exec(ctx)
}

// DeleteSlackLink removes a link (an inbound one whose turn could not start, so a redelivery
// may try again).
func (d *Datastore) DeleteSlackLink(ctx context.Context, linkID uuid.UUID) error {
	return d.dbClient.SlackMessageLink.DeleteOneID(linkID).Exec(ctx)
}

// FindInboundSlackLink returns the inbound link for a saved user message, if it came from
// Slack. A batched turn's message has a link per Slack message; the newest is returned, since
// the reply answers it.
func (d *Datastore) FindInboundSlackLink(ctx context.Context, chatMessageID uuid.UUID) (*models.SlackMessageLink, error) {
	row, err := d.dbClient.SlackMessageLink.Query().
		Where(
			slackmessagelink.ChatMessageID(chatMessageID),
			slackmessagelink.DirectionEQ(slackmessagelink.DirectionInbound),
		).
		WithBinding().
		Order(ent.Desc(slackmessagelink.FieldCreatedAt)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("find inbound slack link: %w", err)
	}
	return slackLinkModel(row, row.Edges.Binding.ID), nil
}

// LastInboundSlackLink returns the binding's newest inbound link created after since, or nil:
// the loop guard's previous message (relaykit.LastInbound).
func (d *Datastore) LastInboundSlackLink(ctx context.Context, bindingID uuid.UUID, since time.Time) (*models.SlackMessageLink, error) {
	row, err := d.dbClient.SlackMessageLink.Query().
		Where(
			slackmessagelink.CreatedAtGT(since),
			slackmessagelink.HasBindingWith(slackbinding.ID(bindingID)),
			slackmessagelink.DirectionEQ(slackmessagelink.DirectionInbound),
		).
		Order(ent.Desc(slackmessagelink.FieldCreatedAt)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("last inbound slack link: %w", err)
	}
	return slackLinkModel(row, bindingID), nil
}

// SlackThreadContinues reports whether a message in a Slack thread continues a conversation
// with the persona there, so it is answered without a mention: the persona posted in that
// thread (a sent reply), and answered the same author there. (Slack has no reply to one message
// inside a thread, so this stands in for Buzz's direct-reply rule.) Both are needed: a batched
// turn answers several askers in one thread only, so an asker whose own thread got no reply
// has not started a conversation there.
func (d *Datastore) SlackThreadContinues(ctx context.Context, bindingID uuid.UUID, threadTS, author string) (bool, error) {
	if threadTS == "" || author == "" {
		return false, nil
	}
	posted, err := d.dbClient.SlackMessageLink.Query().
		Where(
			slackmessagelink.HasBindingWith(slackbinding.ID(bindingID)),
			slackmessagelink.DirectionEQ(slackmessagelink.DirectionOutbound),
			slackmessagelink.ThreadTs(threadTS),
			slackmessagelink.StatusEQ(slackmessagelink.StatusSent),
		).
		Exist(ctx)
	if err != nil || !posted {
		return false, err
	}
	return d.dbClient.SlackMessageLink.Query().
		Where(
			slackmessagelink.HasBindingWith(slackbinding.ID(bindingID)),
			slackmessagelink.DirectionEQ(slackmessagelink.DirectionInbound),
			slackmessagelink.ThreadTs(threadTS),
			slackmessagelink.AuthorUserID(author),
			slackmessagelink.ChatMessageIDNotNil(),
		).
		Exist(ctx)
}

// StartOutboundSlackLink records that a reply is being posted to a binding. If it was already
// recorded (a retried hook), the existing link is returned with created false, and the caller
// posts only if it is not yet sent.
func (d *Datastore) StartOutboundSlackLink(ctx context.Context, bindingID, chatMessageID uuid.UUID, threadTS string, replyTo *uuid.UUID) (*models.SlackMessageLink, bool, error) {
	existing, err := d.dbClient.SlackMessageLink.Query().
		Where(
			slackmessagelink.ChatMessageID(chatMessageID),
			slackmessagelink.DirectionEQ(slackmessagelink.DirectionOutbound),
			slackmessagelink.HasBindingWith(slackbinding.ID(bindingID)),
		).
		First(ctx)
	if err == nil {
		return slackLinkModel(existing, bindingID), false, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, fmt.Errorf("find outbound slack link: %w", err)
	}
	row, err := d.dbClient.SlackMessageLink.Create().
		SetBindingID(bindingID).
		SetDirection(slackmessagelink.DirectionOutbound).
		SetChatMessageID(chatMessageID).
		SetThreadTs(threadTS).
		SetStatus(slackmessagelink.StatusPending).
		SetNillableReplyToLinkID(replyTo).
		Save(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("start outbound slack link: %w", err)
	}
	return slackLinkModel(row, bindingID), true, nil
}

// FinishOutboundSlackLink records the outcome of posting a reply. threadTS, when set, is the
// thread the reply went into (a post_to_slack reply starts one with its first message).
func (d *Datastore) FinishOutboundSlackLink(ctx context.Context, linkID uuid.UUID, threadTS string, postedTS []string, postErr error) error {
	upd := d.dbClient.SlackMessageLink.UpdateOneID(linkID).
		AddAttempts(1).
		SetPostedTs(nonNilStrings(postedTS))
	if threadTS != "" {
		upd.SetThreadTs(threadTS)
	}
	if postErr != nil {
		upd.SetStatus(slackmessagelink.StatusFailed).SetError(postErr.Error())
	} else {
		upd.SetStatus(slackmessagelink.StatusSent).ClearError()
	}
	return upd.Exec(ctx)
}

// ListSlackLinksForChat returns the most recent links of one of the user's chats.
func (d *Datastore) ListSlackLinksForChat(ctx context.Context, userID, chatID uuid.UUID, limit int) ([]models.SlackMessageLink, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := d.dbClient.SlackMessageLink.Query().
		Where(slackmessagelink.HasBindingWith(slackbinding.ChatID(chatID), slackBindingOwnedBy(userID))).
		WithBinding().
		Order(ent.Desc(slackmessagelink.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list slack links for chat: %w", err)
	}
	out := make([]models.SlackMessageLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, *slackLinkModel(r, r.Edges.Binding.ID))
	}
	return out, nil
}

// --- pending posts ---

// SetSlackPendingPost asks for the next reply in chatID to be posted to the user's binding
// (replacing an earlier request for the same pair).
func (d *Datastore) SetSlackPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID, source models.SlackPendingPostSource) error {
	if _, err := d.GetSlackBinding(ctx, userID, bindingID); err != nil {
		return err
	}
	live, err := d.dbClient.SlackBinding.Query().
		Where(
			slackbinding.ID(bindingID),
			slackbinding.StatusEQ(slackbinding.StatusActive),
			slackbinding.HasAppWith(slackapp.StatusEQ(slackapp.StatusActive)),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check slack binding: %w", err)
	}
	if !live {
		return ErrSlackBindingPaused
	}
	if err := d.ClearSlackPendingPost(ctx, userID, bindingID, chatID); err != nil {
		return err
	}
	return d.dbClient.SlackPendingPost.Create().
		SetBindingID(bindingID).
		SetChatID(chatID).
		SetSource(slackpendingpost.Source(source)).
		Exec(ctx)
}

// ClearSlackPendingPost withdraws a request to post the next reply.
func (d *Datastore) ClearSlackPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID) error {
	_, err := d.dbClient.SlackPendingPost.Delete().
		Where(
			slackpendingpost.ChatID(chatID),
			slackpendingpost.HasBindingWith(slackbinding.ID(bindingID), slackBindingOwnedBy(userID)),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("clear slack pending post: %w", err)
	}
	return nil
}

// ConsumeSlackPendingPosts removes and returns the bindings that asked for the next reply in
// chatID to be posted. Expired requests are dropped, not returned.
func (d *Datastore) ConsumeSlackPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := d.dbClient.SlackPendingPost.Query().
		Where(slackpendingpost.ChatID(chatID), slackpendingpost.HasBindingWith(slackBindingOwnedBy(userID))).
		WithBinding().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list slack pending posts: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	now := time.Now()
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, r := range rows {
		// Each request is claimed by deleting it: only the caller whose delete removed the row
		// (n == 1) posts, so each request is posted exactly once. A caller whose delete removed
		// nothing (n == 0) lost the race to another and must not post: two reply hooks for one
		// reply (the orphan path) can read the same rows, and before this both posted. A single
		// DELETE of one row by id is atomic on its own, so no transaction is needed.
		n, err := d.dbClient.SlackPendingPost.Delete().Where(slackpendingpost.ID(r.ID)).Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("consume slack pending posts: %w", err)
		}
		if n == 0 || r.ExpiresAt.Before(now) || r.Edges.Binding == nil || seen[r.Edges.Binding.ID] {
			continue
		}
		seen[r.Edges.Binding.ID] = true
		ids = append(ids, r.Edges.Binding.ID)
	}
	return ids, nil
}

// ListSlackPendingPosts returns which of the user's bindings are set to post the next reply in
// chatID (the composer toggle's state).
func (d *Datastore) ListSlackPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := d.dbClient.SlackPendingPost.Query().
		Where(
			slackpendingpost.ChatID(chatID),
			slackpendingpost.ExpiresAtGT(time.Now()),
			slackpendingpost.HasBindingWith(slackBindingOwnedBy(userID)),
		).
		WithBinding().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list slack pending posts: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.Edges.Binding != nil {
			ids = append(ids, r.Edges.Binding.ID)
		}
	}
	return ids, nil
}

// --- provenance (IsExternalRelayChat, ExternalSpeakerForTurn, RelayPlatformForChat) ---

// externalSlackChat reports whether chatID is bound to a Slack channel by one of the user's
// apps, on any client (a transaction's included).
func externalSlackChat(ctx context.Context, client *ent.Client, userID, chatID uuid.UUID) (bool, error) {
	return client.SlackBinding.Query().
		Where(slackbinding.ChatID(chatID), slackBindingOwnedBy(userID)).
		Exist(ctx)
}

// slackSpeakerForTurn is ExternalSpeakerForTurn for Slack relay threads: the display names of
// the authors of the messages the turn answers, falling back (while the turn is starting) to
// the chat's inbound links that have no message yet.
func (d *Datastore) slackSpeakerForTurn(ctx context.Context, userID, chatID, chatMessageID uuid.UUID) (string, error) {
	owned := slackmessagelink.HasBindingWith(slackBindingOwnedBy(userID))
	inbound := slackmessagelink.DirectionEQ(slackmessagelink.DirectionInbound)
	names := func(rows []*ent.SlackMessageLink) string {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.AuthorName)
		}
		return joinSpeakers(out)
	}
	if chatMessageID != uuid.Nil {
		rows, err := d.dbClient.SlackMessageLink.Query().
			Where(slackmessagelink.ChatMessageID(chatMessageID), inbound, owned).
			Order(ent.Asc(slackmessagelink.FieldCreatedAt)).
			Limit(maxBatchSpeakers).
			All(ctx)
		if err != nil {
			return "", fmt.Errorf("find slack speaker: %w", err)
		}
		if len(rows) > 0 {
			return names(rows), nil
		}
	}
	if chatID == uuid.Nil {
		return "", nil
	}
	rows, err := d.dbClient.SlackMessageLink.Query().
		Where(
			slackmessagelink.ChatMessageIDIsNil(),
			inbound,
			slackmessagelink.HasBindingWith(slackbinding.ChatID(chatID)),
			owned,
		).
		Order(ent.Desc(slackmessagelink.FieldCreatedAt)).
		Limit(maxBatchSpeakers).
		All(ctx)
	if err != nil {
		return "", fmt.Errorf("find slack speaker: %w", err)
	}
	slices.Reverse(rows)
	return names(rows), nil
}

// --- helpers ---

func (d *Datastore) ownedSlackApp(ctx context.Context, userID, id uuid.UUID) (*ent.SlackApp, error) {
	row, err := d.dbClient.SlackApp.Query().Where(slackapp.ID(id), slackAppOwnedBy(userID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrSlackAppNotFound
		}
		return nil, fmt.Errorf("get slack app: %w", err)
	}
	return row, nil
}

func (d *Datastore) slackCredentials(row *ent.SlackApp, ownerID uuid.UUID) (*models.SlackAppCredentials, error) {
	bot, err := d.decryptTokenForRead(row.BotToken)
	if err != nil {
		return nil, fmt.Errorf("decrypt slack bot token: %w", err)
	}
	app, err := d.decryptTokenForRead(row.AppToken)
	if err != nil {
		return nil, fmt.Errorf("decrypt slack app token: %w", err)
	}
	return &models.SlackAppCredentials{
		ID:            row.ID,
		OwnerID:       ownerID,
		PersonalityID: row.PersonalityID,
		TeamID:        row.TeamID,
		AppID:         row.AppID,
		BotUserID:     row.BotUserID,
		BotID:         row.BotID,
		BotToken:      bot,
		AppToken:      app,
		Status:        models.SlackAppStatus(row.Status),
	}, nil
}

func slackAppModel(r *ent.SlackApp) *models.SlackApp {
	return &models.SlackApp{
		ID:              r.ID,
		PersonalityID:   r.PersonalityID,
		TeamID:          r.TeamID,
		TeamName:        r.TeamName,
		TeamURL:         r.TeamURL,
		AppID:           r.AppID,
		BotUserID:       r.BotUserID,
		BotID:           r.BotID,
		BotName:         r.BotName,
		Status:          models.SlackAppStatus(r.Status),
		LastError:       r.LastError,
		LastConnectedAt: r.LastConnectedAt,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

func slackBindingModel(r *ent.SlackBinding, appID uuid.UUID) *models.SlackBinding {
	return &models.SlackBinding{
		ID:                r.ID,
		AppID:             appID,
		ChatID:            r.ChatID,
		ChannelID:         r.ChannelID,
		ChannelName:       r.ChannelName,
		IsPrivate:         r.IsPrivate,
		InboundEnabled:    r.InboundEnabled,
		AllowUserIDs:      nonNilStrings(r.AllowUserIds),
		DenyUserIDs:       nonNilStrings(r.DenyUserIds),
		AnswerAgents:      r.AnswerAgents,
		AllowUnrestricted: r.AllowUnrestricted,
		Status:            models.SlackBindingStatus(r.Status),
		LastError:         r.LastError,
		LastActivityAt:    r.LastActivityAt,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}

func slackLinkModel(r *ent.SlackMessageLink, bindingID uuid.UUID) *models.SlackMessageLink {
	return &models.SlackMessageLink{
		ID:            r.ID,
		BindingID:     bindingID,
		Direction:     string(r.Direction),
		ChatMessageID: r.ChatMessageID,
		MessageTS:     r.MessageTs,
		ThreadTS:      r.ThreadTs,
		PostedTS:      nonNilStrings(r.PostedTs),
		AuthorUserID:  r.AuthorUserID,
		AuthorName:    r.AuthorName,
		ReplyToLinkID: r.ReplyToLinkID,
		Status:        models.SlackLinkStatus(r.Status),
		Error:         r.Error,
		Attempts:      r.Attempts,
		AgentDepth:    r.AgentDepth,
		CreatedAt:     r.CreatedAt,
	}
}
