package datastore

import (
	"context"
	"fmt"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/remotesync"
	"github.com/theimaginaryfoundation/what-iff/ent/remotesyncchat"
	"github.com/theimaginaryfoundation/what-iff/ent/remotesyncmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Applying one remote thread to the local account (ADR 0x032 §6). Everything for one thread
// happens in one transaction, together with its link rows, so a failure leaves nothing behind
// and a re-run picks up exactly where the last committed one stopped.
//
// Terms:
//   - The tracking thread is the local thread a remote thread's link points at.
//   - Its head is its newest linked message: the newest message that came from the remote.
//   - A local tail is anything ordered after the head: something written here. A tracking thread
//     with a local tail has diverged, and new remote messages go to a fork instead.

// remoteSyncInsertChunk bounds one bulk insert, well under SQLite's and Postgres's bind limits.
const remoteSyncInsertChunk = 400

// ApplyRemoteSyncThread brings one remote thread's new messages into the user's account.
// batch is oldest first. When the thread has no link yet it must be the whole remote history,
// since a new mirror is created from it and an adopted thread is aligned against it.
func (d *Datastore) ApplyRemoteSyncThread(ctx context.Context, userID uuid.UUID, thread models.RemoteSyncThread, batch []models.RemoteSyncMessage) (*models.RemoteSyncApplyResult, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("start remote sync apply: %w", err)
	}
	res, err := d.applyRemoteSyncThread(ctx, tx, userID, thread, batch)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit remote sync apply: %w", err)
	}
	return res, nil
}

func (d *Datastore) applyRemoteSyncThread(ctx context.Context, tx *ent.Tx, userID uuid.UUID, thread models.RemoteSyncThread, batch []models.RemoteSyncMessage) (*models.RemoteSyncApplyResult, error) {
	syncRow, err := tx.RemoteSync.Query().Where(remotesync.HasOwnerWith(user.ID(userID))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrRemoteSyncNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get remote sync: %w", err)
	}

	link, err := tx.RemoteSyncChat.Query().
		Where(remotesyncchat.RemoteChatID(thread.RemoteChatID), remotesyncchat.HasSyncWith(remotesync.ID(syncRow.ID))).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("get remote sync link: %w", err)
	}

	res := &models.RemoteSyncApplyResult{}
	if link == nil {
		existing, err := tx.Chat.Query().
			Where(entchat.HasOwnerWith(user.ID(userID)), entchat.ImportHash(models.ConversationImportHash(thread.RemoteChatID.String()))).
			First(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return nil, fmt.Errorf("find importable thread: %w", err)
		}
		if existing == nil {
			return d.createRemoteMirror(ctx, tx, userID, syncRow.ID, thread, batch)
		}
		// Adopt a thread an account-export import (or a mirror whose link was removed) already
		// holds: link the prefix it shares with the remote history, then treat the rest as new.
		link, err = tx.RemoteSyncChat.Create().
			SetSyncID(syncRow.ID).
			SetRemoteChatID(thread.RemoteChatID).
			SetChatID(existing.ID).
			SetRemoteName(existing.Name).
			SetRemotePersonalityName(thread.PersonalityName).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("create remote sync link: %w", err)
		}
		if batch, err = d.adoptRemoteHistory(ctx, tx, link, existing.ID, batch); err != nil {
			return nil, err
		}
		res.Outcome = models.RemoteSyncAdopted
	}

	if link.State == remotesyncchat.StateDetached {
		res.Outcome, res.ChatID = models.RemoteSyncDetached, link.ChatID
		return res, d.touchRemoteLink(ctx, tx, link, thread, nil)
	}
	tracking, err := tx.Chat.Query().Where(entchat.ID(link.ChatID), entchat.HasOwnerWith(user.ID(userID))).Only(ctx)
	if ent.IsNotFound(err) {
		// Deleting a mirror locally means "not here": stop copying it rather than recreate it.
		if err := tx.RemoteSyncChat.UpdateOne(link).SetState(remotesyncchat.StateDetached).Exec(ctx); err != nil {
			return nil, fmt.Errorf("detach remote sync link: %w", err)
		}
		res.Outcome, res.ChatID = models.RemoteSyncDetached, link.ChatID
		return res, d.touchRemoteLink(ctx, tx, link, thread, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("get tracking thread: %w", err)
	}

	batch, err = dropLinkedRemoteMessages(ctx, tx, tracking.ID, batch)
	if err != nil {
		return nil, err
	}
	res.ChatID = tracking.ID
	if len(batch) == 0 {
		if res.Outcome == "" {
			res.Outcome = models.RemoteSyncUnchanged
		}
		return res, d.touchRemoteLink(ctx, tx, link, thread, tracking)
	}

	head, err := remoteSyncHead(ctx, tx, tracking.ID)
	if err != nil {
		return nil, err
	}
	diverged, err := hasLocalTail(ctx, tx, tracking.ID, head)
	if err != nil {
		return nil, err
	}

	target := tracking
	if diverged {
		if target, err = d.forkRemoteMirror(ctx, tx, userID, link, tracking, head, thread); err != nil {
			return nil, err
		}
		res.Outcome = models.RemoteSyncForked
	} else if res.Outcome == "" {
		res.Outcome = models.RemoteSyncAppended
	}
	added, err := insertRemoteMessages(ctx, tx, link.ID, target.ID, batch)
	if err != nil {
		return nil, err
	}
	if err := bumpLastMessageTime(ctx, tx, target, batch); err != nil {
		return nil, err
	}
	res.ChatID, res.MessagesAdded = target.ID, added
	if err := d.touchRemoteLink(ctx, tx, link, thread, target); err != nil {
		return nil, err
	}
	if err := setRemoteHead(ctx, tx, link.ID, batch); err != nil {
		return nil, err
	}
	return res, nil
}

// createRemoteMirror makes a new local thread for a remote one and copies its whole history.
// It is an imported thread (source remote_sync, no summary), so it is summarized lazily the
// first time it is used here, and its import hash lets a later account import skip it.
func (d *Datastore) createRemoteMirror(ctx context.Context, tx *ent.Tx, userID, syncID uuid.UUID, thread models.RemoteSyncThread, batch []models.RemoteSyncMessage) (*models.RemoteSyncApplyResult, error) {
	created := thread.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	create := tx.Chat.Create().
		SetName(models.NormalizeImportedTitle(thread.Name, "Synced thread")).
		SetOwnerID(userID).
		SetSource(models.ChatSourceRemoteSync).
		SetImportHash(models.ConversationImportHash(thread.RemoteChatID.String())).
		SetArchived(thread.Archived).
		SetCreatedAt(created)
	if pid, err := matchPersonalityByName(ctx, tx.Personality, userID, thread.PersonalityName); err != nil {
		return nil, err
	} else if pid != nil {
		create.SetPersonalityID(*pid)
	}
	mirror, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create mirror thread: %w", err)
	}
	link, err := tx.RemoteSyncChat.Create().
		SetSyncID(syncID).
		SetRemoteChatID(thread.RemoteChatID).
		SetChatID(mirror.ID).
		SetRemoteName(thread.Name).
		SetRemotePersonalityName(thread.PersonalityName).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create remote sync link: %w", err)
	}
	added, err := insertRemoteMessages(ctx, tx, link.ID, mirror.ID, batch)
	if err != nil {
		return nil, err
	}
	if err := bumpLastMessageTime(ctx, tx, mirror, batch); err != nil {
		return nil, err
	}
	if err := d.touchRemoteLink(ctx, tx, link, thread, nil); err != nil {
		return nil, err
	}
	if err := setRemoteHead(ctx, tx, link.ID, batch); err != nil {
		return nil, err
	}
	return &models.RemoteSyncApplyResult{Outcome: models.RemoteSyncCreated, ChatID: mirror.ID, MessagesAdded: added}, nil
}

// adoptRemoteHistory links the oldest-first prefix of a local thread's messages that matches the
// remote history (same origin and text), and returns the remote messages left over. Matching
// stops at the first difference: anything local after it is a local tail, anything remote after
// it is new.
func (d *Datastore) adoptRemoteHistory(ctx context.Context, tx *ent.Tx, link *ent.RemoteSyncChat, chatID uuid.UUID, history []models.RemoteSyncMessage) ([]models.RemoteSyncMessage, error) {
	local, err := tx.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(chatID))).
		Order(ent.Asc(chatmessage.FieldSentAt), ent.Asc(chatmessage.FieldID)).
		Select(chatmessage.FieldID, chatmessage.FieldOrigin, chatmessage.FieldMessage).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load thread to adopt: %w", err)
	}
	n := 0
	for n < len(local) && n < len(history) &&
		string(local[n].Origin) == string(history[n].Origin) &&
		strings.TrimSpace(local[n].Message) == strings.TrimSpace(history[n].Message) {
		n++
	}
	for start := 0; start < n; start += remoteSyncInsertChunk {
		end := min(start+remoteSyncInsertChunk, n)
		err := tx.RemoteSyncMessage.MapCreateBulk(local[start:end], func(c *ent.RemoteSyncMessageCreate, i int) {
			r := history[start+i]
			c.SetLinkID(link.ID).SetChatID(chatID).SetChatMessageID(local[start+i].ID).
				SetRemoteMessageID(r.RemoteID).SetRemoteSentAt(r.SentAt)
		}).Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("link adopted messages: %w", err)
		}
	}
	if n > 0 {
		if err := tx.RemoteSyncChat.UpdateOne(link).SetRemoteHeadSentAt(history[n-1].SentAt).Exec(ctx); err != nil {
			return nil, fmt.Errorf("set adopted head: %w", err)
		}
	}
	return history[n:], nil
}

// forkRemoteMirror copies a diverged tracking thread up to and including its head into a new
// thread, links the copies, and points the link at the new thread. The old thread keeps its
// local continuation and is no longer tracked. This is "What if…" branching (PR #148) done by the
// syncer; when that lands, set its forked_from_* columns here so the branch UI shows it.
func (d *Datastore) forkRemoteMirror(ctx context.Context, tx *ent.Tx, userID uuid.UUID, link *ent.RemoteSyncChat, from *ent.Chat, head *ent.ChatMessage, thread models.RemoteSyncThread) (*ent.Chat, error) {
	label := thread.Name
	if label == "" {
		label = from.Name
	}
	if thread.Host != "" {
		label += " (from " + thread.Host + ")"
	}
	create := tx.Chat.Create().
		SetName(models.NormalizeImportedTitle(label, "Synced thread")).
		SetOwnerID(userID).
		SetSource(models.ChatSourceRemoteSync).
		SetArchived(from.Archived).
		SetTags(from.Tags)
	if pid, err := from.QueryPersonality().OnlyID(ctx); err == nil {
		create.SetPersonalityID(pid)
	} else if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("read thread persona: %w", err)
	}
	fork, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create fork thread: %w", err)
	}

	if head != nil {
		prefix, err := tx.ChatMessage.Query().
			Where(chatmessage.HasChatWith(entchat.ID(from.ID)), notAfter(head)).
			Order(ent.Asc(chatmessage.FieldSentAt), ent.Asc(chatmessage.FieldID)).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load fork prefix: %w", err)
		}
		links, err := tx.RemoteSyncMessage.Query().Where(remotesyncmessage.ChatID(from.ID)).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load fork prefix links: %w", err)
		}
		byLocal := make(map[uuid.UUID]*ent.RemoteSyncMessage, len(links))
		for _, l := range links {
			byLocal[l.ChatMessageID] = l
		}
		if err := copyMessagesInto(ctx, tx, link.ID, fork.ID, prefix, byLocal); err != nil {
			return nil, err
		}
	}

	err = tx.RemoteSyncChat.UpdateOne(link).
		SetChatID(fork.ID).
		SetForkedFromChatID(from.ID).
		AddForks(1).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("point link at fork: %w", err)
	}
	return fork, nil
}

// copyMessagesInto copies messages (text and generation names, as the mirror holds them) into a
// thread, carrying over the remote link of any message that has one.
func copyMessagesInto(ctx context.Context, tx *ent.Tx, linkID, chatID uuid.UUID, msgs []*ent.ChatMessage, byLocal map[uuid.UUID]*ent.RemoteSyncMessage) error {
	for start := 0; start < len(msgs); start += remoteSyncInsertChunk {
		part := msgs[start:min(start+remoteSyncInsertChunk, len(msgs))]
		ids := make([]uuid.UUID, len(part))
		err := tx.ChatMessage.MapCreateBulk(part, func(c *ent.ChatMessageCreate, i int) {
			ids[i] = uuid.New()
			m := part[i]
			c.SetID(ids[i]).SetChatID(chatID).SetMessage(m.Message).SetOrigin(m.Origin).
				SetReadStatus(chatmessage.ReadStatusRead).SetSentAt(m.SentAt).
				SetGenerationModel(m.GenerationModel).SetGenerationPersonality(m.GenerationPersonality)
		}).Exec(ctx)
		if err != nil {
			return fmt.Errorf("copy messages: %w", err)
		}
		var links []*ent.RemoteSyncMessageCreate
		for i, m := range part {
			if l := byLocal[m.ID]; l != nil {
				links = append(links, tx.RemoteSyncMessage.Create().
					SetLinkID(linkID).SetChatID(chatID).SetChatMessageID(ids[i]).
					SetRemoteMessageID(l.RemoteMessageID).SetRemoteSentAt(l.RemoteSentAt))
			}
		}
		if len(links) > 0 {
			if err := tx.RemoteSyncMessage.CreateBulk(links...).Exec(ctx); err != nil {
				return fmt.Errorf("copy message links: %w", err)
			}
		}
	}
	return nil
}

// insertRemoteMessages appends remote messages to a thread and links each one.
func insertRemoteMessages(ctx context.Context, tx *ent.Tx, linkID, chatID uuid.UUID, batch []models.RemoteSyncMessage) (int, error) {
	for start := 0; start < len(batch); start += remoteSyncInsertChunk {
		part := batch[start:min(start+remoteSyncInsertChunk, len(batch))]
		ids := make([]uuid.UUID, len(part))
		err := tx.ChatMessage.MapCreateBulk(part, func(c *ent.ChatMessageCreate, i int) {
			ids[i] = uuid.New()
			m := part[i]
			c.SetID(ids[i]).SetChatID(chatID).SetMessage(m.Message).
				SetOrigin(chatmessage.Origin(m.Origin)).SetReadStatus(chatmessage.ReadStatusRead).
				SetSentAt(m.SentAt.UTC()).
				SetGenerationModel(m.GenerationModel).SetGenerationPersonality(m.GenerationPersonality)
		}).Exec(ctx)
		if err != nil {
			return 0, fmt.Errorf("insert remote messages: %w", err)
		}
		err = tx.RemoteSyncMessage.MapCreateBulk(part, func(c *ent.RemoteSyncMessageCreate, i int) {
			c.SetLinkID(linkID).SetChatID(chatID).SetChatMessageID(ids[i]).
				SetRemoteMessageID(part[i].RemoteID).SetRemoteSentAt(part[i].SentAt.UTC())
		}).Exec(ctx)
		if err != nil {
			return 0, fmt.Errorf("link remote messages: %w", err)
		}
	}
	return len(batch), nil
}

// dropLinkedRemoteMessages removes from batch the remote messages already copied into chatID.
// The syncer re-reads a small overlap before its last head on purpose; this makes that free.
func dropLinkedRemoteMessages(ctx context.Context, tx *ent.Tx, chatID uuid.UUID, batch []models.RemoteSyncMessage) ([]models.RemoteSyncMessage, error) {
	if len(batch) == 0 {
		return batch, nil
	}
	ids := make([]uuid.UUID, len(batch))
	for i, m := range batch {
		ids[i] = m.RemoteID
	}
	seen := map[uuid.UUID]bool{}
	for start := 0; start < len(ids); start += remoteSyncInsertChunk {
		got, err := tx.RemoteSyncMessage.Query().
			Where(remotesyncmessage.ChatID(chatID), remotesyncmessage.RemoteMessageIDIn(ids[start:min(start+remoteSyncInsertChunk, len(ids))]...)).
			Select(remotesyncmessage.FieldRemoteMessageID).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("check copied messages: %w", err)
		}
		for _, l := range got {
			seen[l.RemoteMessageID] = true
		}
	}
	out := batch[:0:0]
	for _, m := range batch {
		if !seen[m.RemoteID] {
			out = append(out, m)
		}
	}
	return out, nil
}

// remoteSyncHead returns the newest message in chatID that came from the remote, or nil.
func remoteSyncHead(ctx context.Context, tx *ent.Tx, chatID uuid.UUID) (*ent.ChatMessage, error) {
	head, err := tx.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(chatID)), func(s *entsql.Selector) {
			t := entsql.Table(remotesyncmessage.Table)
			s.Where(entsql.In(s.C(chatmessage.FieldID),
				entsql.Select(t.C(remotesyncmessage.FieldChatMessageID)).From(t).
					Where(entsql.EQ(t.C(remotesyncmessage.FieldChatID), chatID))))
		}).
		Order(ent.Desc(chatmessage.FieldSentAt), ent.Desc(chatmessage.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find remote head: %w", err)
	}
	return head, nil
}

// hasLocalTail reports whether chatID holds a message ordered after head (any message at all
// when there is no head). Everything after the head was written here, by definition.
func hasLocalTail(ctx context.Context, tx *ent.Tx, chatID uuid.UUID, head *ent.ChatMessage) (bool, error) {
	q := tx.ChatMessage.Query().Where(chatmessage.HasChatWith(entchat.ID(chatID)))
	if head != nil {
		q = q.Where(chatmessage.Not(notAfter(head)))
	}
	ok, err := q.Exist(ctx)
	if err != nil {
		return false, fmt.Errorf("check local tail: %w", err)
	}
	return ok, nil
}

// notAfter matches messages at or before m in (sent_at, id) order, the order threads are read in.
func notAfter(m *ent.ChatMessage) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		s.Where(entsql.Or(
			entsql.LT(s.C(chatmessage.FieldSentAt), m.SentAt),
			entsql.And(entsql.EQ(s.C(chatmessage.FieldSentAt), m.SentAt), entsql.LTE(s.C(chatmessage.FieldID), m.ID)),
		))
	}
}

// matchPersonalityByName finds the user's persona with this name (trimmed, case-insensitive), as
// account import does. Remote persona prompts are not served to a chat:read token, so a name is
// all there is to match on; no match leaves the thread on the local default.
func matchPersonalityByName(ctx context.Context, personas *ent.PersonalityClient, userID uuid.UUID, name string) (*uuid.UUID, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	id, err := personas.Query().
		Where(personality.HasUserWith(user.ID(userID)), personality.NameEqualFold(name)).
		Order(ent.Asc(personality.FieldCreatedAt)).
		FirstID(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("match persona: %w", err)
	}
	return &id, nil
}

// bumpLastMessageTime moves a thread's last_message_time forward to the newest copied message,
// so it sorts by activity like any other thread.
func bumpLastMessageTime(ctx context.Context, tx *ent.Tx, c *ent.Chat, batch []models.RemoteSyncMessage) error {
	if len(batch) == 0 {
		return nil
	}
	latest := batch[len(batch)-1].SentAt
	for _, m := range batch {
		if m.SentAt.After(latest) {
			latest = m.SentAt
		}
	}
	if c.LastMessageTime != nil && !latest.After(*c.LastMessageTime) {
		return nil
	}
	if err := tx.Chat.UpdateOneID(c.ID).SetLastMessageTime(latest).Exec(ctx); err != nil {
		return fmt.Errorf("update last message time: %w", err)
	}
	return nil
}

// touchRemoteLink records what the syncer saw of the remote thread (its last_message_time and
// name), and copies a remote rename onto the tracking thread while the local name is still the
// last remote one, so a local rename wins.
func (d *Datastore) touchRemoteLink(ctx context.Context, tx *ent.Tx, link *ent.RemoteSyncChat, thread models.RemoteSyncThread, tracking *ent.Chat) error {
	upd := tx.RemoteSyncChat.UpdateOneID(link.ID)
	if thread.PersonalityName != "" && thread.PersonalityName != link.RemotePersonalityName {
		upd.SetRemotePersonalityName(thread.PersonalityName)
	}
	if thread.LastMessageTime != nil {
		upd.SetRemoteLastMessageTime(*thread.LastMessageTime)
	}
	if thread.Name != "" && thread.Name != link.RemoteName {
		upd.SetRemoteName(thread.Name)
		if tracking != nil && tracking.Name == link.RemoteName {
			err := tx.Chat.UpdateOneID(tracking.ID).
				SetName(models.NormalizeImportedTitle(thread.Name, tracking.Name)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("rename tracking thread: %w", err)
			}
		}
	}
	if err := upd.Exec(ctx); err != nil {
		return fmt.Errorf("update remote sync link: %w", err)
	}
	return nil
}

// setRemoteHead moves the link's pull position to the newest message in batch, never back.
func setRemoteHead(ctx context.Context, tx *ent.Tx, linkID uuid.UUID, batch []models.RemoteSyncMessage) error {
	if len(batch) == 0 {
		return nil
	}
	latest := batch[0].SentAt
	for _, m := range batch[1:] {
		if m.SentAt.After(latest) {
			latest = m.SentAt
		}
	}
	err := tx.RemoteSyncChat.Update().
		Where(remotesyncchat.ID(linkID), remotesyncchat.Or(
			remotesyncchat.RemoteHeadSentAtIsNil(),
			remotesyncchat.RemoteHeadSentAtLT(latest),
		)).
		SetRemoteHeadSentAt(latest.UTC()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("set remote head: %w", err)
	}
	return nil
}
