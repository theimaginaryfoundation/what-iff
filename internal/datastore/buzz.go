package datastore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/buzzagent"
	"github.com/theimaginaryfoundation/what-iff/ent/buzzbinding"
	"github.com/theimaginaryfoundation/what-iff/ent/buzzcommunity"
	"github.com/theimaginaryfoundation/what-iff/ent/buzzmessagelink"
	"github.com/theimaginaryfoundation/what-iff/ent/buzzpendingpost"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Buzz relay storage (docs/adr/0x028-buzz-relay.md). As for Discord, methods taking a userID
// are owner-scoped: another account's agent, community, binding or chat is not found. Methods
// without one are for the relay itself (the gateway and the reply hook), which act on behalf
// of the owner recorded on the agent.

var (
	ErrBuzzAgentNotFound     = errors.New("buzz agent not found")
	ErrBuzzCommunityNotFound = errors.New("buzz community not found")
	ErrBuzzBindingNotFound   = errors.New("buzz binding not found")
	// ErrBuzzAgentExists: the persona already has a Buzz identity.
	ErrBuzzAgentExists = errors.New("persona already connected to buzz")
	// ErrBuzzCommunityExists: the persona is already in (or joining) that community.
	ErrBuzzCommunityExists = errors.New("buzz community already added for this persona")
	// ErrBuzzBindingExists: the channel is already bound for that community.
	ErrBuzzBindingExists = errors.New("buzz channel already bound for this persona")
	// ErrBuzzBindingPaused: the binding is broken, or its persona or community is paused, so
	// nothing may be posted through it.
	ErrBuzzBindingPaused = errors.New("buzz binding is paused or broken")
)

func buzzAgentOwnedBy(userID uuid.UUID) predicate.BuzzAgent {
	return buzzagent.HasOwnerWith(user.ID(userID))
}

func buzzCommunityOwnedBy(userID uuid.UUID) predicate.BuzzCommunity {
	return buzzcommunity.HasAgentWith(buzzAgentOwnedBy(userID))
}

func buzzBindingOwnedBy(userID uuid.UUID) predicate.BuzzBinding {
	return buzzbinding.HasCommunityWith(buzzCommunityOwnedBy(userID))
}

// --- agents ---

// CreateBuzzAgent stores a persona's Buzz identity, encrypting its secret key.
func (d *Datastore) CreateBuzzAgent(ctx context.Context, userID uuid.UUID, in models.BuzzAgentCreate) (*models.BuzzAgent, error) {
	if err := d.requireOwnedPersonality(ctx, userID, in.PersonalityID); err != nil {
		return nil, err
	}
	enc, err := d.encryptTokenForWrite(in.SecretKey)
	if err != nil {
		return nil, err
	}
	row, err := d.dbClient.BuzzAgent.Create().
		SetOwnerID(userID).
		SetPersonalityID(in.PersonalityID).
		SetPubkey(in.PubKey).
		SetSecretKey(enc).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrBuzzAgentExists
		}
		return nil, fmt.Errorf("create buzz agent: %w", err)
	}
	return buzzAgentModel(row), nil
}

// ListBuzzAgents returns the user's Buzz identities, oldest first.
func (d *Datastore) ListBuzzAgents(ctx context.Context, userID uuid.UUID) ([]models.BuzzAgent, error) {
	rows, err := d.dbClient.BuzzAgent.Query().
		Where(buzzAgentOwnedBy(userID)).
		Order(ent.Asc(buzzagent.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buzz agents: %w", err)
	}
	out := make([]models.BuzzAgent, 0, len(rows))
	for _, r := range rows {
		out = append(out, *buzzAgentModel(r))
	}
	return out, nil
}

// GetBuzzAgent returns one of the user's Buzz identities.
func (d *Datastore) GetBuzzAgent(ctx context.Context, userID, id uuid.UUID) (*models.BuzzAgent, error) {
	row, err := d.ownedBuzzAgent(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return buzzAgentModel(row), nil
}

// GetBuzzAgentCredentials returns one of the user's Buzz identities with its secret key.
func (d *Datastore) GetBuzzAgentCredentials(ctx context.Context, userID, id uuid.UUID) (*models.BuzzAgentCredentials, error) {
	row, err := d.ownedBuzzAgent(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return d.buzzCredentials(row, userID)
}

// SetBuzzAgentStatus pauses or resumes one of the user's Buzz identities.
func (d *Datastore) SetBuzzAgentStatus(ctx context.Context, userID, id uuid.UUID, status models.BuzzAgentStatus) (*models.BuzzAgent, error) {
	if _, err := d.ownedBuzzAgent(ctx, userID, id); err != nil {
		return nil, err
	}
	row, err := d.dbClient.BuzzAgent.UpdateOneID(id).SetStatus(buzzagent.Status(status)).ClearLastError().Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update buzz agent: %w", err)
	}
	if status == models.BuzzAgentDisabled {
		if err := d.forgetBuzzResume(ctx, buzzbinding.HasCommunityWith(buzzcommunity.HasAgentWith(buzzagent.ID(id)))); err != nil {
			return nil, err
		}
	}
	return buzzAgentModel(row), nil
}

// forgetBuzzResume clears the gateway's resume point on bindings that were just paused or
// broken, in the write that does it: whichever process leads the gateway next, listening again
// starts from now, not from before the pause. (MarkBuzzBindingEvent refuses such bindings, so a
// late write cannot bring the mark back.)
func (d *Datastore) forgetBuzzResume(ctx context.Context, where ...predicate.BuzzBinding) error {
	if _, err := d.dbClient.BuzzBinding.Update().Where(where...).ClearLastEventAt().Save(ctx); err != nil {
		return fmt.Errorf("clear buzz resume points: %w", err)
	}
	return nil
}

// DeleteBuzzAgent removes one of the user's Buzz identities and, by cascade, its communities,
// bindings and links. The key is gone for good: Buzz will know the persona as a new member if
// it is connected again.
func (d *Datastore) DeleteBuzzAgent(ctx context.Context, userID, id uuid.UUID) error {
	n, err := d.dbClient.BuzzAgent.Delete().Where(buzzagent.ID(id), buzzAgentOwnedBy(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete buzz agent: %w", err)
	}
	if n == 0 {
		return ErrBuzzAgentNotFound
	}
	return nil
}

// --- communities ---

// CreateBuzzCommunity adds a community for one of the user's Buzz identities.
func (d *Datastore) CreateBuzzCommunity(ctx context.Context, userID, agentID uuid.UUID, in models.BuzzCommunityCreate) (*models.BuzzCommunity, error) {
	if _, err := d.ownedBuzzAgent(ctx, userID, agentID); err != nil {
		return nil, err
	}
	create := d.dbClient.BuzzCommunity.Create().
		SetAgentID(agentID).
		SetRelayURL(in.RelayURL).
		SetName(in.Name).
		SetRelayPubkey(in.RelayPubKey).
		SetStatus(buzzcommunity.Status(in.Status)).
		SetNillableOwnerPubkey(in.OwnerPubKey).
		SetNillablePolicyVersion(in.PolicyVersion).
		SetNillableLastError(in.LastError)
	if in.AuthTag != nil {
		enc, err := d.encryptTokenForWrite(*in.AuthTag)
		if err != nil {
			return nil, err
		}
		create.SetAuthTag(enc)
	}
	row, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrBuzzCommunityExists
		}
		return nil, fmt.Errorf("create buzz community: %w", err)
	}
	return buzzCommunityModel(row, agentID), nil
}

// ListBuzzCommunities returns the communities of all the user's Buzz identities, oldest first.
func (d *Datastore) ListBuzzCommunities(ctx context.Context, userID uuid.UUID) ([]models.BuzzCommunity, error) {
	rows, err := d.dbClient.BuzzCommunity.Query().
		Where(buzzCommunityOwnedBy(userID)).
		WithAgent().
		Order(ent.Asc(buzzcommunity.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buzz communities: %w", err)
	}
	out := make([]models.BuzzCommunity, 0, len(rows))
	for _, r := range rows {
		out = append(out, *buzzCommunityModel(r, r.Edges.Agent.ID))
	}
	return out, nil
}

// GetBuzzCommunity returns one of the user's communities.
func (d *Datastore) GetBuzzCommunity(ctx context.Context, userID, id uuid.UUID) (*models.BuzzCommunity, error) {
	row, err := d.ownedBuzzCommunity(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return buzzCommunityModel(row, row.Edges.Agent.ID), nil
}

// GetBuzzConnection returns one of the user's communities with what acting in it takes: the
// persona's credentials and attestation (no channel list).
func (d *Datastore) GetBuzzConnection(ctx context.Context, userID, id uuid.UUID) (*models.BuzzConnection, error) {
	row, err := d.ownedBuzzCommunity(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	creds, err := d.buzzCredentials(row.Edges.Agent, userID)
	if err != nil {
		return nil, err
	}
	tag, err := d.buzzAuthTag(row)
	if err != nil {
		return nil, err
	}
	return &models.BuzzConnection{Agent: *creds, Community: *buzzCommunityModel(row, row.Edges.Agent.ID), AuthTag: tag}, nil
}

// UpdateBuzzCommunity changes one of the user's communities.
func (d *Datastore) UpdateBuzzCommunity(ctx context.Context, userID, id uuid.UUID, p models.BuzzCommunityPatch) (*models.BuzzCommunity, error) {
	if _, err := d.ownedBuzzCommunity(ctx, userID, id); err != nil {
		return nil, err
	}
	return d.patchBuzzCommunity(ctx, id, p)
}

// SetBuzzCommunityState records how the connection stands (the gateway's view). An empty
// lastError clears it; an active state records the connection time. A community the owner
// paused is left paused: a connection closing just after the pause cannot undo it.
func (d *Datastore) SetBuzzCommunityState(ctx context.Context, id uuid.UUID, status models.BuzzCommunityStatus, lastError string) error {
	upd := d.dbClient.BuzzCommunity.Update().
		Where(buzzcommunity.ID(id), buzzcommunity.StatusNEQ(buzzcommunity.StatusDisabled)).
		SetStatus(buzzcommunity.Status(status))
	if lastError == "" {
		upd.ClearLastError()
	} else {
		upd.SetLastError(lastError)
	}
	if status == models.BuzzCommunityActive {
		upd.SetLastConnectedAt(time.Now())
	}
	if _, err := upd.Save(ctx); err != nil {
		return fmt.Errorf("set buzz community state: %w", err)
	}
	return nil
}

// MarkBuzzBindingEvent moves a binding's resume point forward to at (never back), only while
// the gateway should be listening to it: inbound on, active, its community and persona not
// paused. A write that arrives after a pause therefore cannot bring a cleared mark back.
func (d *Datastore) MarkBuzzBindingEvent(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := d.dbClient.BuzzBinding.Update().
		Where(
			buzzbinding.ID(id),
			buzzbinding.Or(buzzbinding.LastEventAtIsNil(), buzzbinding.LastEventAtLT(at)),
			buzzbinding.InboundEnabled(true),
			buzzbinding.StatusEQ(buzzbinding.StatusActive),
			buzzbinding.HasCommunityWith(
				buzzcommunity.StatusNEQ(buzzcommunity.StatusDisabled),
				buzzcommunity.HasAgentWith(buzzagent.StatusEQ(buzzagent.StatusActive)),
			),
		).
		SetLastEventAt(at).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("mark buzz binding event: %w", err)
	}
	return nil
}

// ClearBuzzBindingEvents forgets the resume points of bindings the gateway stopped listening
// to, so listening again starts from now.
func (d *Datastore) ClearBuzzBindingEvents(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := d.dbClient.BuzzBinding.Update().Where(buzzbinding.IDIn(ids...)).ClearLastEventAt().Save(ctx); err != nil {
		return fmt.Errorf("clear buzz binding events: %w", err)
	}
	return nil
}

func (d *Datastore) patchBuzzCommunity(ctx context.Context, id uuid.UUID, p models.BuzzCommunityPatch) (*models.BuzzCommunity, error) {
	upd := d.dbClient.BuzzCommunity.UpdateOneID(id)
	if p.Name != nil {
		upd.SetName(*p.Name)
	}
	if p.RelayPubKey != nil {
		upd.SetRelayPubkey(*p.RelayPubKey)
	}
	if p.AuthTag != nil {
		if *p.AuthTag == "" {
			upd.ClearAuthTag()
		} else {
			enc, err := d.encryptTokenForWrite(*p.AuthTag)
			if err != nil {
				return nil, err
			}
			upd.SetAuthTag(enc)
		}
	}
	if p.OwnerPubKey != nil {
		if *p.OwnerPubKey == "" {
			upd.ClearOwnerPubkey()
		} else {
			upd.SetOwnerPubkey(*p.OwnerPubKey)
		}
	}
	if p.PolicyVersion != nil {
		upd.SetPolicyVersion(*p.PolicyVersion)
	}
	if p.Status != nil {
		upd.SetStatus(buzzcommunity.Status(*p.Status))
		if *p.Status == models.BuzzCommunityDisabled {
			if err := d.forgetBuzzResume(ctx, buzzbinding.HasCommunityWith(buzzcommunity.ID(id))); err != nil {
				return nil, err
			}
		}
	}
	if p.LastError != nil {
		if *p.LastError == "" {
			upd.ClearLastError()
		} else {
			upd.SetLastError(*p.LastError)
		}
	}
	if p.ConnectedNow {
		upd.SetLastConnectedAt(time.Now())
	}
	row, err := upd.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrBuzzCommunityNotFound
		}
		return nil, fmt.Errorf("update buzz community: %w", err)
	}
	agentID, err := row.QueryAgent().OnlyID(ctx)
	if err != nil {
		return nil, fmt.Errorf("buzz community agent: %w", err)
	}
	return buzzCommunityModel(row, agentID), nil
}

// DeleteBuzzCommunity removes one of the user's communities and, by cascade, its bindings.
func (d *Datastore) DeleteBuzzCommunity(ctx context.Context, userID, id uuid.UUID) error {
	n, err := d.dbClient.BuzzCommunity.Delete().Where(buzzcommunity.ID(id), buzzCommunityOwnedBy(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete buzz community: %w", err)
	}
	if n == 0 {
		return ErrBuzzCommunityNotFound
	}
	return nil
}

// IsBuzzAgentPubKey reports whether pubkey is any persona's Buzz identity on this server, of
// any owner (the relay's loop guard: personas do not answer each other unless asked to).
func (d *Datastore) IsBuzzAgentPubKey(ctx context.Context, pubkey string) (bool, error) {
	ok, err := d.dbClient.BuzzAgent.Query().Where(buzzagent.Pubkey(pubkey)).Exist(ctx)
	if err != nil {
		return false, fmt.Errorf("find buzz agent by pubkey: %w", err)
	}
	return ok, nil
}

// ListActiveBuzzConnections returns every community the gateway should hold a connection for:
// those of active identities that the owner has not paused, each with its bound channels
// (active, inbound on). A community without bindings is still connected, so its access state
// stays current. An identity whose key cannot be decrypted is skipped rather than failing the
// whole list.
func (d *Datastore) ListActiveBuzzConnections(ctx context.Context) ([]models.BuzzConnection, error) {
	rows, err := d.dbClient.BuzzCommunity.Query().
		Where(
			buzzcommunity.StatusNEQ(buzzcommunity.StatusDisabled),
			buzzcommunity.HasAgentWith(buzzagent.StatusEQ(buzzagent.StatusActive)),
		).
		WithAgent(func(q *ent.BuzzAgentQuery) { q.WithOwner() }).
		WithBindings(func(q *ent.BuzzBindingQuery) {
			q.Where(buzzbinding.InboundEnabled(true), buzzbinding.StatusEQ(buzzbinding.StatusActive))
		}).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active buzz communities: %w", err)
	}
	out := make([]models.BuzzConnection, 0, len(rows))
	for _, r := range rows {
		agent := r.Edges.Agent
		if agent == nil || agent.Edges.Owner == nil {
			continue
		}
		creds, err := d.buzzCredentials(agent, agent.Edges.Owner.ID)
		if err != nil {
			d.logger.Warn("buzz agent key unreadable; skipping")
			continue
		}
		tag, err := d.buzzAuthTag(r)
		if err != nil {
			d.logger.Warn("buzz attestation unreadable; skipping")
			continue
		}
		conn := models.BuzzConnection{Agent: *creds, Community: *buzzCommunityModel(r, agent.ID), AuthTag: tag}
		for _, b := range r.Edges.Bindings {
			conn.Channels = append(conn.Channels, models.BuzzListenChannel{BindingID: b.ID, ChannelID: b.ChannelID, BoundAt: b.CreatedAt, LastEventAt: b.LastEventAt})
		}
		out = append(out, conn)
	}
	return out, nil
}

// --- bindings ---

// CreateBuzzBinding binds a channel of one of the user's communities to one of the user's chats.
func (d *Datastore) CreateBuzzBinding(ctx context.Context, userID, communityID uuid.UUID, in models.BuzzBindingCreate) (*models.BuzzBinding, error) {
	if _, err := d.ownedBuzzCommunity(ctx, userID, communityID); err != nil {
		return nil, err
	}
	if err := d.requireOwnedChat(ctx, userID, in.ChatID); err != nil {
		return nil, err
	}
	row, err := d.dbClient.BuzzBinding.Create().
		SetCommunityID(communityID).
		SetChatID(in.ChatID).
		SetChannelID(in.ChannelID).
		SetChannelName(in.ChannelName).
		SetInboundEnabled(in.InboundEnabled).
		SetAllowPubkeys(nonNilStrings(in.AllowPubKeys)).
		SetDenyPubkeys(nonNilStrings(in.DenyPubKeys)).
		SetAnswerAgents(in.AnswerAgents).
		SetAllowUnrestricted(in.AllowUnrestricted).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrBuzzBindingExists
		}
		return nil, fmt.Errorf("create buzz binding: %w", err)
	}
	return buzzBindingModel(row, communityID), nil
}

// ListBuzzBindings returns all of the user's bindings, oldest first.
func (d *Datastore) ListBuzzBindings(ctx context.Context, userID uuid.UUID) ([]models.BuzzBinding, error) {
	return d.listBuzzBindings(ctx, buzzBindingOwnedBy(userID))
}

// ListBuzzBindingsForChat returns the user's bindings whose relay thread is chatID.
func (d *Datastore) ListBuzzBindingsForChat(ctx context.Context, userID, chatID uuid.UUID) ([]models.BuzzBinding, error) {
	return d.listBuzzBindings(ctx, buzzBindingOwnedBy(userID), buzzbinding.ChatID(chatID))
}

// ListBuzzBindingsForPersonality returns the user's active bindings whose identity speaks as
// personalityID, in communities that are not paused: where that persona can post.
func (d *Datastore) ListBuzzBindingsForPersonality(ctx context.Context, userID, personalityID uuid.UUID) ([]models.BuzzBinding, error) {
	return d.listBuzzBindings(ctx, buzzbinding.HasCommunityWith(
		buzzcommunity.StatusNEQ(buzzcommunity.StatusDisabled),
		buzzcommunity.HasAgentWith(
			buzzAgentOwnedBy(userID),
			buzzagent.PersonalityID(personalityID),
			buzzagent.StatusEQ(buzzagent.StatusActive),
		),
	))
}

func (d *Datastore) listBuzzBindings(ctx context.Context, where ...predicate.BuzzBinding) ([]models.BuzzBinding, error) {
	rows, err := d.dbClient.BuzzBinding.Query().
		Where(where...).
		WithCommunity().
		Order(ent.Asc(buzzbinding.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buzz bindings: %w", err)
	}
	out := make([]models.BuzzBinding, 0, len(rows))
	for _, r := range rows {
		m := buzzBindingModel(r, r.Edges.Community.ID)
		m.CommunityName = r.Edges.Community.Name
		out = append(out, *m)
	}
	return out, nil
}

// GetBuzzBinding returns one of the user's bindings.
func (d *Datastore) GetBuzzBinding(ctx context.Context, userID, id uuid.UUID) (*models.BuzzBinding, error) {
	row, err := d.dbClient.BuzzBinding.Query().
		Where(buzzbinding.ID(id), buzzBindingOwnedBy(userID)).
		WithCommunity().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrBuzzBindingNotFound
		}
		return nil, fmt.Errorf("get buzz binding: %w", err)
	}
	return buzzBindingModel(row, row.Edges.Community.ID), nil
}

// UpdateBuzzBinding changes one of the user's bindings. Moving it to another chat checks that
// chat is the user's.
func (d *Datastore) UpdateBuzzBinding(ctx context.Context, userID, id uuid.UUID, p models.BuzzBindingPatch) (*models.BuzzBinding, error) {
	if _, err := d.GetBuzzBinding(ctx, userID, id); err != nil {
		return nil, err
	}
	if p.ChatID != nil {
		if err := d.requireOwnedChat(ctx, userID, *p.ChatID); err != nil {
			return nil, err
		}
	}
	return d.patchBuzzBinding(ctx, id, p)
}

// WithdrawBuzzBindingAcknowledgement clears a binding's "not sandboxed is fine" acknowledgement
// (the relay does when it sees the thread sandboxed again).
func (d *Datastore) WithdrawBuzzBindingAcknowledgement(ctx context.Context, id uuid.UUID) error {
	off := false
	_, err := d.patchBuzzBinding(ctx, id, models.BuzzBindingPatch{AllowUnrestricted: &off})
	return err
}

// SetBuzzBindingStatus records a binding's health from the relay. An empty lastError clears it.
func (d *Datastore) SetBuzzBindingStatus(ctx context.Context, id uuid.UUID, status models.BuzzBindingStatus, lastError string) error {
	_, err := d.patchBuzzBinding(ctx, id, models.BuzzBindingPatch{Status: &status, LastError: &lastError})
	return err
}

// RenameBuzzChannel records a channel's current name (relay-signed kind 39000) on a binding,
// when it changed.
func (d *Datastore) RenameBuzzChannel(ctx context.Context, bindingID uuid.UUID, name string) error {
	name = strings.TrimPrefix(strings.TrimSpace(name), "#")
	if name == "" {
		return nil
	}
	_, err := d.dbClient.BuzzBinding.Update().
		Where(buzzbinding.ID(bindingID), buzzbinding.ChannelNameNEQ(name)).
		SetChannelName(name).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("rename buzz channel: %w", err)
	}
	return nil
}

// TouchBuzzBinding records activity on a binding.
func (d *Datastore) TouchBuzzBinding(ctx context.Context, id uuid.UUID) error {
	return d.dbClient.BuzzBinding.UpdateOneID(id).SetLastActivityAt(time.Now()).Exec(ctx)
}

func (d *Datastore) patchBuzzBinding(ctx context.Context, id uuid.UUID, p models.BuzzBindingPatch) (*models.BuzzBinding, error) {
	upd := d.dbClient.BuzzBinding.UpdateOneID(id)
	if p.ChatID != nil {
		upd.SetChatID(*p.ChatID)
	}
	if p.ChannelName != nil {
		upd.SetChannelName(*p.ChannelName)
	}
	if p.InboundEnabled != nil {
		upd.SetInboundEnabled(*p.InboundEnabled)
	}
	if p.AllowPubKeys != nil {
		upd.SetAllowPubkeys(nonNilStrings(*p.AllowPubKeys))
	}
	if p.DenyPubKeys != nil {
		upd.SetDenyPubkeys(nonNilStrings(*p.DenyPubKeys))
	}
	if p.AnswerAgents != nil {
		upd.SetAnswerAgents(*p.AnswerAgents)
	}
	if p.AllowUnrestricted != nil {
		upd.SetAllowUnrestricted(*p.AllowUnrestricted)
	}
	if p.Status != nil {
		upd.SetStatus(buzzbinding.Status(*p.Status))
	}
	// Inbound off or broken: the gateway stops listening, so forget where it was (as in
	// forgetBuzzResume).
	if (p.InboundEnabled != nil && !*p.InboundEnabled) || (p.Status != nil && *p.Status == models.BuzzBindingBroken) {
		upd.ClearLastEventAt()
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
			return nil, ErrBuzzBindingNotFound
		}
		return nil, fmt.Errorf("update buzz binding: %w", err)
	}
	communityID, err := row.QueryCommunity().OnlyID(ctx)
	if err != nil {
		return nil, fmt.Errorf("buzz binding community: %w", err)
	}
	return buzzBindingModel(row, communityID), nil
}

// DeleteBuzzBinding removes one of the user's bindings.
func (d *Datastore) DeleteBuzzBinding(ctx context.Context, userID, id uuid.UUID) error {
	n, err := d.dbClient.BuzzBinding.Delete().Where(buzzbinding.ID(id), buzzBindingOwnedBy(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete buzz binding: %w", err)
	}
	if n == 0 {
		return ErrBuzzBindingNotFound
	}
	return nil
}

// FindBuzzBindingTarget resolves the binding a community has for a channel, with the persona's
// credentials and owner. ErrBuzzBindingNotFound when the channel is not bound there.
func (d *Datastore) FindBuzzBindingTarget(ctx context.Context, communityID uuid.UUID, channelID string) (*models.BuzzBindingTarget, error) {
	return d.buzzBindingTarget(ctx, buzzbinding.ChannelID(channelID), buzzbinding.HasCommunityWith(buzzcommunity.ID(communityID)))
}

// GetBuzzBindingTarget resolves a binding by id for the relay.
func (d *Datastore) GetBuzzBindingTarget(ctx context.Context, bindingID uuid.UUID) (*models.BuzzBindingTarget, error) {
	return d.buzzBindingTarget(ctx, buzzbinding.ID(bindingID))
}

func (d *Datastore) buzzBindingTarget(ctx context.Context, where ...predicate.BuzzBinding) (*models.BuzzBindingTarget, error) {
	row, err := d.dbClient.BuzzBinding.Query().
		Where(where...).
		WithCommunity(func(q *ent.BuzzCommunityQuery) {
			q.WithAgent(func(q *ent.BuzzAgentQuery) { q.WithOwner() })
		}).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrBuzzBindingNotFound
		}
		return nil, fmt.Errorf("find buzz binding: %w", err)
	}
	community := row.Edges.Community
	if community == nil || community.Edges.Agent == nil || community.Edges.Agent.Edges.Owner == nil {
		return nil, ErrBuzzBindingNotFound
	}
	agent := community.Edges.Agent
	creds, err := d.buzzCredentials(agent, agent.Edges.Owner.ID)
	if err != nil {
		return nil, err
	}
	tag, err := d.buzzAuthTag(community)
	if err != nil {
		return nil, err
	}
	return &models.BuzzBindingTarget{
		Binding:   *buzzBindingModel(row, community.ID),
		Community: *buzzCommunityModel(community, agent.ID),
		Agent:     *creds,
		AuthTag:   tag,
	}, nil
}

// --- message links ---

// RecordInboundBuzzMessage records an event that is about to start a turn. created is false
// when it was already recorded (a redelivered event): the caller must not start another turn.
func (d *Datastore) RecordInboundBuzzMessage(ctx context.Context, in models.BuzzInbound) (link *models.BuzzMessageLink, created bool, err error) {
	row, err := d.dbClient.BuzzMessageLink.Create().
		SetBindingID(in.BindingID).
		SetDirection(buzzmessagelink.DirectionInbound).
		SetEventID(in.EventID).
		SetThreadRoot(in.ThreadRoot).
		SetAuthorPubkey(in.AuthorPubKey).
		SetAuthorName(in.AuthorName).
		SetAgentDepth(in.AgentDepth).
		SetStatus(buzzmessagelink.StatusReceived).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("record inbound buzz message: %w", err)
	}
	return buzzLinkModel(row, in.BindingID), true, nil
}

// AttachBuzzLinkMessage records the What Iff message an inbound link became.
func (d *Datastore) AttachBuzzLinkMessage(ctx context.Context, linkID, chatMessageID uuid.UUID) error {
	return d.dbClient.BuzzMessageLink.UpdateOneID(linkID).SetChatMessageID(chatMessageID).Exec(ctx)
}

// DeleteBuzzLink removes a link (an inbound one whose turn could not start, so a redelivery
// may try again).
func (d *Datastore) DeleteBuzzLink(ctx context.Context, linkID uuid.UUID) error {
	return d.dbClient.BuzzMessageLink.DeleteOneID(linkID).Exec(ctx)
}

// FindInboundBuzzLink returns the inbound link for a saved user message, if it came from Buzz.
// A batched turn's message has a link per event; the newest is returned, since the reply
// answers it.
func (d *Datastore) FindInboundBuzzLink(ctx context.Context, chatMessageID uuid.UUID) (*models.BuzzMessageLink, error) {
	row, err := d.dbClient.BuzzMessageLink.Query().
		Where(
			buzzmessagelink.ChatMessageID(chatMessageID),
			buzzmessagelink.DirectionEQ(buzzmessagelink.DirectionInbound),
		).
		WithBinding().
		Order(ent.Desc(buzzmessagelink.FieldCreatedAt)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("find inbound buzz link: %w", err)
	}
	return buzzLinkModel(row, row.Edges.Binding.ID), nil
}

// LastInboundBuzzLink returns the binding's newest inbound link created after since, or nil:
// the loop guard's previous message (relaykit.LastInbound).
func (d *Datastore) LastInboundBuzzLink(ctx context.Context, bindingID uuid.UUID, since time.Time) (*models.BuzzMessageLink, error) {
	row, err := d.dbClient.BuzzMessageLink.Query().
		Where(
			buzzmessagelink.CreatedAtGT(since),
			buzzmessagelink.HasBindingWith(buzzbinding.ID(bindingID)),
			buzzmessagelink.DirectionEQ(buzzmessagelink.DirectionInbound),
		).
		Order(ent.Desc(buzzmessagelink.FieldCreatedAt)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("last inbound buzz link: %w", err)
	}
	return buzzLinkModel(row, bindingID), nil
}

// BuzzThreadContinues reports whether a message in a bound channel continues a conversation
// with the persona there, so it should be answered without a mention: it replies directly to
// one of the persona's messages (parent), or it is the same person writing on in the thread of
// a message of theirs the persona answered (root).
func (d *Datastore) BuzzThreadContinues(ctx context.Context, bindingID uuid.UUID, root, parent, author string) (bool, error) {
	onBinding := buzzmessagelink.HasBindingWith(buzzbinding.ID(bindingID))
	if parent != "" {
		ok, err := d.dbClient.BuzzMessageLink.Query().
			Where(
				onBinding,
				buzzmessagelink.DirectionEQ(buzzmessagelink.DirectionOutbound),
				func(s *entsql.Selector) {
					s.Where(sqljson.ValueContains(buzzmessagelink.FieldPostedEventIds, parent))
				},
			).
			Exist(ctx)
		if err != nil || ok {
			return ok, err
		}
	}
	if root == "" || author == "" {
		return false, nil
	}
	return d.dbClient.BuzzMessageLink.Query().
		Where(
			onBinding,
			buzzmessagelink.DirectionEQ(buzzmessagelink.DirectionInbound),
			buzzmessagelink.EventID(root),
			buzzmessagelink.AuthorPubkey(author),
			buzzmessagelink.ChatMessageIDNotNil(),
		).
		Exist(ctx)
}

// StartOutboundBuzzLink records that a reply is being posted to a binding. If it was already
// recorded (a retried hook), the existing link is returned with created false, and the caller
// posts only if it is not yet sent.
func (d *Datastore) StartOutboundBuzzLink(ctx context.Context, bindingID, chatMessageID uuid.UUID, threadRoot string, replyTo *uuid.UUID) (*models.BuzzMessageLink, bool, error) {
	existing, err := d.dbClient.BuzzMessageLink.Query().
		Where(
			buzzmessagelink.ChatMessageID(chatMessageID),
			buzzmessagelink.DirectionEQ(buzzmessagelink.DirectionOutbound),
			buzzmessagelink.HasBindingWith(buzzbinding.ID(bindingID)),
		).
		First(ctx)
	if err == nil {
		return buzzLinkModel(existing, bindingID), false, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, fmt.Errorf("find outbound buzz link: %w", err)
	}
	row, err := d.dbClient.BuzzMessageLink.Create().
		SetBindingID(bindingID).
		SetDirection(buzzmessagelink.DirectionOutbound).
		SetChatMessageID(chatMessageID).
		SetThreadRoot(threadRoot).
		SetStatus(buzzmessagelink.StatusPending).
		SetNillableReplyToLinkID(replyTo).
		Save(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("start outbound buzz link: %w", err)
	}
	return buzzLinkModel(row, bindingID), true, nil
}

// FinishOutboundBuzzLink records the outcome of posting a reply.
func (d *Datastore) FinishOutboundBuzzLink(ctx context.Context, linkID uuid.UUID, postedIDs []string, postErr error) error {
	upd := d.dbClient.BuzzMessageLink.UpdateOneID(linkID).
		AddAttempts(1).
		SetPostedEventIds(nonNilStrings(postedIDs))
	if postErr != nil {
		upd.SetStatus(buzzmessagelink.StatusFailed).SetError(postErr.Error())
	} else {
		upd.SetStatus(buzzmessagelink.StatusSent).ClearError()
	}
	return upd.Exec(ctx)
}

// ListBuzzLinksForChat returns the most recent links of one of the user's chats.
func (d *Datastore) ListBuzzLinksForChat(ctx context.Context, userID, chatID uuid.UUID, limit int) ([]models.BuzzMessageLink, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := d.dbClient.BuzzMessageLink.Query().
		Where(buzzmessagelink.HasBindingWith(buzzbinding.ChatID(chatID), buzzBindingOwnedBy(userID))).
		WithBinding().
		Order(ent.Desc(buzzmessagelink.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buzz links for chat: %w", err)
	}
	out := make([]models.BuzzMessageLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, *buzzLinkModel(r, r.Edges.Binding.ID))
	}
	return out, nil
}

// --- pending posts ---

// SetBuzzPendingPost asks for the next reply in chatID to be posted to the user's binding
// (replacing an earlier request for the same pair).
func (d *Datastore) SetBuzzPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID, source models.BuzzPendingPostSource) error {
	if _, err := d.GetBuzzBinding(ctx, userID, bindingID); err != nil {
		return err
	}
	live, err := d.dbClient.BuzzBinding.Query().
		Where(
			buzzbinding.ID(bindingID),
			buzzbinding.StatusEQ(buzzbinding.StatusActive),
			buzzbinding.HasCommunityWith(
				buzzcommunity.StatusNEQ(buzzcommunity.StatusDisabled),
				buzzcommunity.HasAgentWith(buzzagent.StatusEQ(buzzagent.StatusActive)),
			),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check buzz binding: %w", err)
	}
	if !live {
		return ErrBuzzBindingPaused
	}
	if err := d.ClearBuzzPendingPost(ctx, userID, bindingID, chatID); err != nil {
		return err
	}
	return d.dbClient.BuzzPendingPost.Create().
		SetBindingID(bindingID).
		SetChatID(chatID).
		SetSource(buzzpendingpost.Source(source)).
		Exec(ctx)
}

// ClearBuzzPendingPost withdraws a request to post the next reply.
func (d *Datastore) ClearBuzzPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID) error {
	_, err := d.dbClient.BuzzPendingPost.Delete().
		Where(
			buzzpendingpost.ChatID(chatID),
			buzzpendingpost.HasBindingWith(buzzbinding.ID(bindingID), buzzBindingOwnedBy(userID)),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("clear buzz pending post: %w", err)
	}
	return nil
}

// ConsumeBuzzPendingPosts removes and returns the bindings that asked for the next reply in
// chatID to be posted. Expired requests are dropped, not returned.
func (d *Datastore) ConsumeBuzzPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := d.dbClient.BuzzPendingPost.Query().
		Where(buzzpendingpost.ChatID(chatID), buzzpendingpost.HasBindingWith(buzzBindingOwnedBy(userID))).
		WithBinding().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buzz pending posts: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	now := time.Now()
	seen := map[uuid.UUID]bool{}
	var ids, rowIDs []uuid.UUID
	for _, r := range rows {
		rowIDs = append(rowIDs, r.ID)
		if r.ExpiresAt.Before(now) || r.Edges.Binding == nil || seen[r.Edges.Binding.ID] {
			continue
		}
		seen[r.Edges.Binding.ID] = true
		ids = append(ids, r.Edges.Binding.ID)
	}
	if _, err := d.dbClient.BuzzPendingPost.Delete().Where(buzzpendingpost.IDIn(rowIDs...)).Exec(ctx); err != nil {
		return nil, fmt.Errorf("consume buzz pending posts: %w", err)
	}
	return ids, nil
}

// ListBuzzPendingPosts returns which of the user's bindings are set to post the next reply in
// chatID (the composer toggle's state).
func (d *Datastore) ListBuzzPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := d.dbClient.BuzzPendingPost.Query().
		Where(
			buzzpendingpost.ChatID(chatID),
			buzzpendingpost.ExpiresAtGT(time.Now()),
			buzzpendingpost.HasBindingWith(buzzBindingOwnedBy(userID)),
		).
		WithBinding().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buzz pending posts: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.Edges.Binding != nil {
			ids = append(ids, r.Edges.Binding.ID)
		}
	}
	return ids, nil
}

// --- provenance (IsExternalRelayChat, ExternalSpeakerForTurn) ---

// externalBuzzChat reports whether chatID is bound to a Buzz channel by one of the user's
// personas, on any client (a transaction's included).
func externalBuzzChat(ctx context.Context, client *ent.Client, userID, chatID uuid.UUID) (bool, error) {
	return client.BuzzBinding.Query().
		Where(buzzbinding.ChatID(chatID), buzzBindingOwnedBy(userID)).
		Exist(ctx)
}

// buzzSpeakerForTurn is ExternalSpeakerForTurn for Buzz relay threads: the display names of the
// authors of the messages the turn answers, falling back (while the turn is starting) to the
// chat's inbound links that have no message yet.
func (d *Datastore) buzzSpeakerForTurn(ctx context.Context, userID, chatID, chatMessageID uuid.UUID) (string, error) {
	owned := buzzmessagelink.HasBindingWith(buzzBindingOwnedBy(userID))
	inbound := buzzmessagelink.DirectionEQ(buzzmessagelink.DirectionInbound)
	names := func(rows []*ent.BuzzMessageLink) string {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.AuthorName)
		}
		return joinSpeakers(out)
	}
	if chatMessageID != uuid.Nil {
		rows, err := d.dbClient.BuzzMessageLink.Query().
			Where(buzzmessagelink.ChatMessageID(chatMessageID), inbound, owned).
			Order(ent.Asc(buzzmessagelink.FieldCreatedAt)).
			Limit(maxBatchSpeakers).
			All(ctx)
		if err != nil {
			return "", fmt.Errorf("find buzz speaker: %w", err)
		}
		if len(rows) > 0 {
			return names(rows), nil
		}
	}
	if chatID == uuid.Nil {
		return "", nil
	}
	rows, err := d.dbClient.BuzzMessageLink.Query().
		Where(
			buzzmessagelink.ChatMessageIDIsNil(),
			inbound,
			buzzmessagelink.HasBindingWith(buzzbinding.ChatID(chatID)),
			owned,
		).
		Order(ent.Desc(buzzmessagelink.FieldCreatedAt)).
		Limit(maxBatchSpeakers).
		All(ctx)
	if err != nil {
		return "", fmt.Errorf("find buzz speaker: %w", err)
	}
	slices.Reverse(rows)
	return names(rows), nil
}

// RelayPlatformForChat names the platform a relay thread is relayed from ("Discord", "Buzz"),
// or "" for an ordinary chat. It picks the wording of the memory extraction note.
func (d *Datastore) RelayPlatformForChat(ctx context.Context, userID, chatID uuid.UUID) (string, error) {
	if chatID == uuid.Nil {
		return "", nil
	}
	buzz, err := externalBuzzChat(ctx, d.dbClient, userID, chatID)
	if err != nil {
		return "", err
	}
	if buzz {
		return "Buzz", nil
	}
	discord, err := externalRelayChat(ctx, d.dbClient, userID, chatID)
	if err != nil || !discord {
		return "", err
	}
	return "Discord", nil
}

// --- helpers ---

func (d *Datastore) ownedBuzzAgent(ctx context.Context, userID, id uuid.UUID) (*ent.BuzzAgent, error) {
	row, err := d.dbClient.BuzzAgent.Query().Where(buzzagent.ID(id), buzzAgentOwnedBy(userID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrBuzzAgentNotFound
		}
		return nil, fmt.Errorf("get buzz agent: %w", err)
	}
	return row, nil
}

func (d *Datastore) ownedBuzzCommunity(ctx context.Context, userID, id uuid.UUID) (*ent.BuzzCommunity, error) {
	row, err := d.dbClient.BuzzCommunity.Query().
		Where(buzzcommunity.ID(id), buzzCommunityOwnedBy(userID)).
		WithAgent().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrBuzzCommunityNotFound
		}
		return nil, fmt.Errorf("get buzz community: %w", err)
	}
	return row, nil
}

func (d *Datastore) buzzCredentials(row *ent.BuzzAgent, ownerID uuid.UUID) (*models.BuzzAgentCredentials, error) {
	secret, err := d.decryptTokenForRead(row.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt buzz agent key: %w", err)
	}
	return &models.BuzzAgentCredentials{
		ID:            row.ID,
		OwnerID:       ownerID,
		PersonalityID: row.PersonalityID,
		PubKey:        row.Pubkey,
		SecretKey:     secret,
		Status:        models.BuzzAgentStatus(row.Status),
	}, nil
}

// buzzAuthTag is a community's decrypted attestation, "" when it has none.
func (d *Datastore) buzzAuthTag(row *ent.BuzzCommunity) (string, error) {
	if row.AuthTag == nil || *row.AuthTag == "" {
		return "", nil
	}
	tag, err := d.decryptTokenForRead(*row.AuthTag)
	if err != nil {
		return "", fmt.Errorf("decrypt buzz attestation: %w", err)
	}
	return tag, nil
}

func buzzAgentModel(r *ent.BuzzAgent) *models.BuzzAgent {
	m := &models.BuzzAgent{
		ID:            r.ID,
		PersonalityID: r.PersonalityID,
		PubKey:        r.Pubkey,
		Status:        models.BuzzAgentStatus(r.Status),
		LastError:     r.LastError,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
	m.NPub, _ = nostr.EncodeNPub(r.Pubkey)
	return m
}

func buzzCommunityModel(r *ent.BuzzCommunity, agentID uuid.UUID) *models.BuzzCommunity {
	return &models.BuzzCommunity{
		ID:              r.ID,
		AgentID:         agentID,
		RelayURL:        r.RelayURL,
		Name:            r.Name,
		RelayPubKey:     r.RelayPubkey,
		Attested:        r.AuthTag != nil && *r.AuthTag != "",
		OwnerPubKey:     r.OwnerPubkey,
		PolicyVersion:   r.PolicyVersion,
		Status:          models.BuzzCommunityStatus(r.Status),
		LastError:       r.LastError,
		LastConnectedAt: r.LastConnectedAt,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

func buzzBindingModel(r *ent.BuzzBinding, communityID uuid.UUID) *models.BuzzBinding {
	return &models.BuzzBinding{
		ID:                r.ID,
		CommunityID:       communityID,
		ChatID:            r.ChatID,
		ChannelID:         r.ChannelID,
		ChannelName:       r.ChannelName,
		InboundEnabled:    r.InboundEnabled,
		AllowPubKeys:      nonNilStrings(r.AllowPubkeys),
		DenyPubKeys:       nonNilStrings(r.DenyPubkeys),
		AnswerAgents:      r.AnswerAgents,
		AllowUnrestricted: r.AllowUnrestricted,
		Status:            models.BuzzBindingStatus(r.Status),
		LastError:         r.LastError,
		LastActivityAt:    r.LastActivityAt,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}

func buzzLinkModel(r *ent.BuzzMessageLink, bindingID uuid.UUID) *models.BuzzMessageLink {
	return &models.BuzzMessageLink{
		ID:             r.ID,
		BindingID:      bindingID,
		Direction:      string(r.Direction),
		ChatMessageID:  r.ChatMessageID,
		EventID:        r.EventID,
		ThreadRoot:     r.ThreadRoot,
		PostedEventIDs: nonNilStrings(r.PostedEventIds),
		AuthorPubKey:   r.AuthorPubkey,
		AuthorName:     r.AuthorName,
		ReplyToLinkID:  r.ReplyToLinkID,
		Status:         models.BuzzLinkStatus(r.Status),
		Error:          r.Error,
		Attempts:       r.Attempts,
		AgentDepth:     r.AgentDepth,
		CreatedAt:      r.CreatedAt,
	}
}
