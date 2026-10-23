package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/weave"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Weaves (docs/adr/0x030-thread-weaves.md): named sets of sandboxed threads of one owner and one
// personality that may read each other. Membership is the chats' weave_id column; a chat is in at
// most one weave. Every method is owner-scoped.
//
// What a weave widens is the sandbox's READ boundary only. The rules that keep writes inside the
// thread that makes them (memoryOfChat in merges, Chat-scoped writes) are untouched.

var (
	ErrWeaveNotFound = errors.New("weave not found")
	// ErrWeaveChat is a chat that cannot join (or is not in) the weave; the wrapped text says why.
	ErrWeaveChat = errors.New("weave: chat cannot join")
)

// CreateWeave makes a weave of the user's chats (at least two).
func (d *Datastore) CreateWeave(ctx context.Context, userID uuid.UUID, name string, chatIDs []uuid.UUID) (*models.Weave, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: a weave needs a name", ErrWeaveChat)
	}
	chatIDs = uniqueIDs(chatIDs)
	if len(chatIDs) < 2 {
		return nil, fmt.Errorf("%w: a weave joins at least two threads", ErrWeaveChat)
	}
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	chats, err := weavableChats(ctx, tx.Client(), userID, chatIDs)
	if err != nil {
		return nil, err
	}
	if err := samePersonality(chats); err != nil {
		return nil, err
	}
	row, err := tx.Weave.Create().SetOwnerID(userID).SetName(name).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create weave: %w", err)
	}
	if err := tx.Chat.Update().Where(entchat.IDIn(chatIDs...)).SetWeaveID(row.ID).Exec(ctx); err != nil {
		return nil, fmt.Errorf("join weave: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetWeave(ctx, userID, row.ID)
}

// GetWeave returns one of the user's weaves with its members.
func (d *Datastore) GetWeave(ctx context.Context, userID, id uuid.UUID) (*models.Weave, error) {
	row, err := d.ownedWeave(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return d.weaveModel(ctx, userID, row)
}

// ListWeaves returns the user's weaves with their members, oldest first.
func (d *Datastore) ListWeaves(ctx context.Context, userID uuid.UUID) ([]models.Weave, error) {
	rows, err := d.dbClient.Weave.Query().
		Where(weave.HasOwnerWith(user.ID(userID))).
		Order(ent.Asc(weave.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list weaves: %w", err)
	}
	out := make([]models.Weave, 0, len(rows))
	for _, r := range rows {
		w, err := d.weaveModel(ctx, userID, r)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, nil
}

// AddChatToWeave adds one of the user's chats to a weave: it must be sandboxed, in no other weave,
// and of the same personality as the weave's members.
func (d *Datastore) AddChatToWeave(ctx context.Context, userID, weaveID, chatID uuid.UUID) (*models.Weave, error) {
	if _, err := d.ownedWeave(ctx, userID, weaveID); err != nil {
		return nil, err
	}
	chats, err := weavableChats(ctx, d.dbClient, userID, []uuid.UUID{chatID})
	if err != nil {
		if !errors.Is(err, ErrWeaveChat) {
			return nil, err // not found, or a storage failure
		}
		// Refused for membership: being in this very weave already is not an error.
		member, gerr := d.dbClient.Chat.Query().
			Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID)), entchat.WeaveID(weaveID)).
			Exist(ctx)
		if gerr != nil {
			return nil, fmt.Errorf("check weave membership: %w", gerr)
		}
		if member {
			return d.GetWeave(ctx, userID, weaveID)
		}
		return nil, err
	}
	members, err := d.dbClient.Chat.Query().
		Where(entchat.WeaveID(weaveID), entchat.HasOwnerWith(user.ID(userID))).
		WithPersonality().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list weave members: %w", err)
	}
	if err := samePersonality(append(members, chats...)); err != nil {
		return nil, err
	}
	if err := d.dbClient.Chat.UpdateOneID(chatID).SetWeaveID(weaveID).Exec(ctx); err != nil {
		return nil, fmt.Errorf("join weave: %w", err)
	}
	return d.GetWeave(ctx, userID, weaveID)
}

// RemoveChatFromWeave takes one of the user's chats out of a weave. From its next read on, the
// chat sees only itself again, and its peers no longer see it; nothing it read is copied, so
// nothing needs undoing (memory context persisted on earlier turns is re-checked on replay).
func (d *Datastore) RemoveChatFromWeave(ctx context.Context, userID, weaveID, chatID uuid.UUID) (*models.Weave, error) {
	if _, err := d.ownedWeave(ctx, userID, weaveID); err != nil {
		return nil, err
	}
	n, err := d.dbClient.Chat.Update().
		Where(entchat.ID(chatID), entchat.WeaveID(weaveID), entchat.HasOwnerWith(user.ID(userID))).
		ClearWeaveID().
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("leave weave: %w", err)
	}
	if n == 0 {
		return nil, fmt.Errorf("%w: the thread is not in this weave", ErrWeaveChat)
	}
	return d.GetWeave(ctx, userID, weaveID)
}

// DeleteWeave unweaves its members and removes the weave.
func (d *Datastore) DeleteWeave(ctx context.Context, userID, id uuid.UUID) error {
	if _, err := d.ownedWeave(ctx, userID, id); err != nil {
		return err
	}
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.Chat.Update().Where(entchat.WeaveID(id), entchat.HasOwnerWith(user.ID(userID))).ClearWeaveID().Exec(ctx); err != nil {
		return fmt.Errorf("unweave: %w", err)
	}
	if err := tx.Weave.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("delete weave: %w", err)
	}
	return tx.Commit()
}

// weavePeers returns the chats a sandboxed chat in a weave may also read: the other SANDBOXED chats
// of the same owner and personality in the same weave. A chat that left the sandbox or changed
// personality since it joined drops out (fail closed). Not sandboxed or not in a weave: none.
func weavePeers(ctx context.Context, client *ent.Client, userID, chatID uuid.UUID) ([]models.ChatRef, error) {
	if chatID == uuid.Nil {
		return nil, nil
	}
	self, err := client.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		WithPersonality().
		Only(ctx)
	if err != nil {
		// Every caller has loaded the chat already, so a missing one is an anomaly (deleted
		// mid-turn, or another account's id): say so rather than read it as "no peers".
		if ent.IsNotFound(err) {
			return nil, ErrChatNotFound
		}
		return nil, fmt.Errorf("load chat for weave: %w", err)
	}
	if self.ContextScope != entchat.ContextScopeSandbox || self.WeaveID == nil || self.Edges.Personality == nil {
		return nil, nil
	}
	rows, err := client.Chat.Query().
		Where(
			entchat.WeaveID(*self.WeaveID),
			entchat.IDNEQ(chatID),
			entchat.HasOwnerWith(user.ID(userID)),
			entchat.ContextScopeEQ(entchat.ContextScopeSandbox),
			entchat.HasPersonalityWith(personality.ID(self.Edges.Personality.ID)),
		).
		Order(ent.Asc(entchat.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load weave peers: %w", err)
	}
	out := make([]models.ChatRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.ChatRef{ID: r.ID, Name: r.Name})
	}
	return out, nil
}

// readableChatIDs is the conversations a chat reads in the sandbox sense: itself and its weave
// peers (just itself when it is in no weave). It backs the SQL scoping of memory and summary
// searches, so a sandbox's search returns a full set of matches from its readable set.
func readableChatIDs(ctx context.Context, client *ent.Client, userID, chatID uuid.UUID) ([]uuid.UUID, error) {
	peers, err := weavePeers(ctx, client, userID, chatID)
	if err != nil {
		return nil, err
	}
	ids := []uuid.UUID{chatID}
	for _, p := range peers {
		ids = append(ids, p.ID)
	}
	return ids, nil
}

// weavableChats loads chats that may join a weave: the user's, sandboxed, in no weave yet. Any
// other is ErrChatNotFound (not the user's) or ErrWeaveChat with the reason.
func weavableChats(ctx context.Context, client *ent.Client, userID uuid.UUID, ids []uuid.UUID) ([]*ent.Chat, error) {
	rows, err := client.Chat.Query().
		Where(entchat.IDIn(ids...), entchat.HasOwnerWith(user.ID(userID))).
		WithPersonality().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load chats for weave: %w", err)
	}
	if len(rows) != len(ids) {
		return nil, ErrChatNotFound
	}
	for _, r := range rows {
		switch {
		case r.ContextScope != entchat.ContextScopeSandbox:
			return nil, fmt.Errorf("%w: %q is not sandboxed (only sandboxed threads can be woven)", ErrWeaveChat, r.Name)
		case r.WeaveID != nil:
			return nil, fmt.Errorf("%w: %q is already in a weave", ErrWeaveChat, r.Name)
		case r.Edges.Personality == nil:
			return nil, fmt.Errorf("%w: %q has no personality", ErrWeaveChat, r.Name)
		}
	}
	return rows, nil
}

// samePersonality checks that chats (with their personality loaded) all speak as one personality.
func samePersonality(chats []*ent.Chat) error {
	var first uuid.UUID
	for _, c := range chats {
		if c.Edges.Personality == nil {
			return fmt.Errorf("%w: %q has no personality", ErrWeaveChat, c.Name)
		}
		if first == uuid.Nil {
			first = c.Edges.Personality.ID
		} else if c.Edges.Personality.ID != first {
			return fmt.Errorf("%w: a weave's threads must all be with the same personality", ErrWeaveChat)
		}
	}
	return nil
}

func (d *Datastore) ownedWeave(ctx context.Context, userID, id uuid.UUID) (*ent.Weave, error) {
	row, err := d.dbClient.Weave.Query().Where(weave.ID(id), weave.HasOwnerWith(user.ID(userID))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrWeaveNotFound
		}
		return nil, fmt.Errorf("get weave: %w", err)
	}
	return row, nil
}

func (d *Datastore) weaveModel(ctx context.Context, userID uuid.UUID, row *ent.Weave) (*models.Weave, error) {
	members, err := d.dbClient.Chat.Query().
		Where(entchat.WeaveID(row.ID), entchat.HasOwnerWith(user.ID(userID))).
		Order(ent.Asc(entchat.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list weave members: %w", err)
	}
	w := &models.Weave{ID: row.ID, Name: row.Name, Chats: []models.ChatRef{}, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	for _, m := range members {
		w.Chats = append(w.Chats, models.ChatRef{ID: m.ID, Name: m.Name})
	}
	return w, nil
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
