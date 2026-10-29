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
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/remotesync"
	"github.com/theimaginaryfoundation/what-iff/ent/remotesyncchat"
	"github.com/theimaginaryfoundation/what-iff/ent/remotesyncmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Applying one remote thread to the local account (ADR 0x033 §6). Everything for one thread
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

// remoteSyncCatchUpAfter is how many messages may sit past a thread's summary before sync flags
// it for a catch-up summary. It matches the agent's history page (defaultHistoryPageSize): a
// reply reads the summary and at most that many newer messages, so anything older than those and
// not in the summary would be invisible to it.
const remoteSyncCatchUpAfter = 50

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

	diverged, err := hasLocalTail(ctx, tx, tracking.ID)
	if err != nil {
		return nil, err
	}

	target := tracking
	if diverged {
		if target, err = d.forkRemoteMirror(ctx, tx, userID, link, tracking, thread); err != nil {
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
	if err := settleRemoteSummary(ctx, tx, target.ID, thread.Summary); err != nil {
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
	if err := settleRemoteSummary(ctx, tx, mirror.ID, thread.Summary); err != nil {
		return nil, err
	}
	return &models.RemoteSyncApplyResult{Outcome: models.RemoteSyncCreated, ChatID: mirror.ID, MessagesAdded: added}, nil
}

// settleRemoteSummary runs once remote messages have landed in a thread nobody has continued here
// (a new mirror, a tracking thread with no local tail, or a fresh fork), so everything in it came
// from the remote. It takes the remote's running summary if that reaches further than the
// thread's own (adoptRemoteSummary), then flags the thread for a catch-up if too much still sits
// past its summary (flagIfBehind). A thread whose rehydration is in flight is left to it.
func settleRemoteSummary(ctx context.Context, tx *ent.Tx, chatID uuid.UUID, remote *models.ChatSummary) error {
	c, err := tx.Chat.Get(ctx, chatID)
	if err != nil {
		return fmt.Errorf("load thread for summary: %w", err)
	}
	if c.RehydrationState == models.RehydrationStatePending || c.RehydrationState == models.RehydrationStateProcessing {
		return nil
	}
	if c, err = adoptRemoteSummary(ctx, tx, c, remote); err != nil || c == nil {
		return err
	}
	return flagIfBehind(ctx, tx, c)
}

// adoptRemoteSummary gives the thread the remote's running summary when that reaches further
// than the thread's own, so the thread needs no summarizing here. It returns the thread as it now
// stands, or nil when a rehydration claimed it meanwhile.
func adoptRemoteSummary(ctx context.Context, tx *ent.Tx, c *ent.Chat, remote *models.ChatSummary) (*ent.Chat, error) {
	if remote == nil || remote.WindowStart == nil || strings.TrimSpace(remote.Summary) == "" ||
		(c.LastCheckpointAt != nil && !remote.WindowStart.After(*c.LastCheckpointAt)) {
		return c, nil
	}
	// The checkpoint cadence counts assistant messages since the last checkpoint.
	assistants, err := tx.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(c.ID)),
			chatmessage.OriginEQ(chatmessage.OriginAssistant),
			chatmessage.SentAtLT(*remote.WindowStart)).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count summarized replies: %w", err)
	}
	n, err := tx.Chat.Update().
		Where(entchat.ID(c.ID), notRehydrating()).
		SetCheckpointSummary(remote.Summary).
		SetLastCheckpointAt(*remote.WindowStart).
		SetCheckpointUserMessageCount(assistants).
		SetRehydrationState(models.RehydrationStateReady).
		ClearResponseID().
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("adopt remote summary: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	updated, err := tx.Chat.Get(ctx, c.ID)
	if err != nil {
		return nil, fmt.Errorf("reload thread after summary: %w", err)
	}
	return updated, nil
}

// flagIfBehind marks a summarized thread stale when more messages sit past its summary than a
// reply reads (the remote summary was missing or behind, or the thread is an export import that
// sync appended to): its next use folds them into the summary. A thread never summarized is left
// to its first rehydration, which summarizes the whole history anyway.
func flagIfBehind(ctx context.Context, tx *ent.Tx, c *ent.Chat) error {
	if c.RehydrationState == models.RehydrationStateStale {
		return nil
	}
	if c.CheckpointSummary == "" &&
		(c.RehydrationState == models.RehydrationStateNone || c.RehydrationState == models.RehydrationStateFailed) {
		return nil
	}
	pending := tx.ChatMessage.Query().Where(chatmessage.HasChatWith(entchat.ID(c.ID)))
	if c.LastCheckpointAt != nil {
		pending = pending.Where(chatmessage.SentAtGTE(*c.LastCheckpointAt))
	}
	n, err := pending.Count(ctx)
	if err != nil {
		return fmt.Errorf("count unsummarized messages: %w", err)
	}
	if n <= remoteSyncCatchUpAfter {
		return nil
	}
	err = tx.Chat.Update().
		Where(entchat.ID(c.ID), notRehydrating()).
		SetRehydrationState(models.RehydrationStateStale).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("flag thread for catch-up: %w", err)
	}
	return nil
}

// notRehydrating matches a chat no rehydration is running for. adoptRemoteSummary's and flagIfBehind's writes are
// conditional on it, like ClaimChatRehydration's claim, so a rehydration claimed after the thread
// was read is never overwritten.
func notRehydrating() predicate.Chat {
	return entchat.Or(
		entchat.RehydrationStateIsNil(),
		entchat.RehydrationStateNotIn(models.RehydrationStatePending, models.RehydrationStateProcessing),
	)
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

// forkRemoteMirror copies the remote messages of a diverged tracking thread into a new thread,
// links the copies, and points the link at the new thread. The old thread keeps its local
// continuation and is no longer tracked. This is "What if…" branching (PR #148) done by the
// syncer; when that lands, set its forked_from_* columns here so the branch UI shows it.
//
// The remote thread's import hash moves to the fork too, so the thread that tracks the remote is
// always the one adoption finds: after Stop syncing and a new save it is re-adopted as it is,
// rather than the old thread being adopted and forked a second time.
func (d *Datastore) forkRemoteMirror(ctx context.Context, tx *ent.Tx, userID uuid.UUID, link *ent.RemoteSyncChat, from *ent.Chat, thread models.RemoteSyncThread) (*ent.Chat, error) {
	label := thread.Name
	if label == "" {
		label = from.Name
	}
	if thread.Host != "" {
		label += " (from " + thread.Host + ")"
	}
	hash := models.ConversationImportHash(thread.RemoteChatID.String())
	// (owner, import_hash) is unique: take the hash off whichever thread holds it first.
	err := tx.Chat.Update().
		Where(entchat.HasOwnerWith(user.ID(userID)), entchat.ImportHash(hash)).
		ClearImportHash().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("move import hash: %w", err)
	}
	create := tx.Chat.Create().
		SetName(models.NormalizeImportedTitle(label, "Synced thread")).
		SetOwnerID(userID).
		SetImportHash(hash).
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

	prefix, err := tx.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(from.ID)), fromRemote(from.ID)).
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

// hasLocalTail reports whether chatID holds a message that did not come from the remote, which
// can only have been written here. Every remote message in a tracking thread is linked (mirror,
// adopted prefix and fork prefix alike), so this needs no comparison of sent_at values written
// by two different clocks: a home server running behind could otherwise file a reply before the
// remote head and miss it.
func hasLocalTail(ctx context.Context, tx *ent.Tx, chatID uuid.UUID) (bool, error) {
	ok, err := tx.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(chatID)), chatmessage.Not(fromRemote(chatID))).
		Exist(ctx)
	if err != nil {
		return false, fmt.Errorf("check local tail: %w", err)
	}
	return ok, nil
}

// fromRemote matches the messages of chatID that are linked to a remote message. The subquery
// seeks the (chat_id, remote_message_id) unique index on remote_sync_messages by its leading
// column, so it reads only this thread's links.
func fromRemote(chatID uuid.UUID) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		t := entsql.Table(remotesyncmessage.Table)
		s.Where(entsql.In(s.C(chatmessage.FieldID),
			entsql.Select(t.C(remotesyncmessage.FieldChatMessageID)).From(t).
				Where(entsql.EQ(t.C(remotesyncmessage.FieldChatID), chatID))))
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
