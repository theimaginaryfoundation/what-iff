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
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/remotesync"
	"github.com/theimaginaryfoundation/what-iff/ent/remotesyncchat"
	"github.com/theimaginaryfoundation/what-iff/ent/remotesyncmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Remote sync storage (docs/adr/0x033-mullet-mode-remote-sync.md): each user's sync settings and lease, and
// the links from remote threads and messages to local ones. Every method is owner-scoped by
// userID. Applying a remote thread is in remotesync_apply.go.

var (
	ErrRemoteSyncNotFound = errors.New("remote sync not configured")
	// ErrRemoteSyncTokenRequired: the first save of an account's sync must include a token.
	ErrRemoteSyncTokenRequired = errors.New("a token is required")
)

// GetRemoteSyncSettings returns the user's sync as the settings page shows it, with totals.
// An account with no sync is Configured: false, not an error.
func (d *Datastore) GetRemoteSyncSettings(ctx context.Context, userID uuid.UUID) (*models.RemoteSyncSettings, error) {
	row, err := d.ownedRemoteSync(ctx, userID)
	if errors.Is(err, ErrRemoteSyncNotFound) {
		return &models.RemoteSyncSettings{Configured: false}, nil
	}
	if err != nil {
		return nil, err
	}
	out := &models.RemoteSyncSettings{
		Configured:    true,
		Enabled:       row.Enabled,
		BaseURL:       row.BaseURL,
		TokenHint:     row.TokenHint,
		Status:        models.RemoteSyncStatus(row.Status),
		LastError:     row.LastError,
		LastRunAt:     row.LastRunAt,
		LastSuccessAt: row.LastSuccessAt,
		Running:       row.LeaseUntil != nil && row.LeaseUntil.After(time.Now()),
	}
	if row.LastRun != nil {
		stats := models.RemoteSyncRunStatsFromMap(row.LastRun)
		out.LastRun = &stats
	}
	totals, err := d.remoteSyncTotals(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	out.Totals = totals
	unmatched, err := d.unmatchedRemoteCopies(ctx, userID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, name := range unmatched {
		if key := strings.ToLower(strings.TrimSpace(name)); !seen[key] {
			seen[key] = true
			out.UnmatchedPersonas = append(out.UnmatchedPersonas, strings.TrimSpace(name))
		}
	}
	slices.Sort(out.UnmatchedPersonas)
	return out, nil
}

func (d *Datastore) remoteSyncTotals(ctx context.Context, syncID uuid.UUID) (*models.RemoteSyncTotals, error) {
	links, err := d.dbClient.RemoteSyncChat.Query().
		Where(remotesyncchat.HasSyncWith(remotesync.ID(syncID))).
		Select(remotesyncchat.FieldState, remotesyncchat.FieldForks).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("count remote sync threads: %w", err)
	}
	totals := &models.RemoteSyncTotals{}
	for _, l := range links {
		if l.State == remotesyncchat.StateActive {
			totals.Threads++
		}
		totals.Forks += l.Forks
	}
	// Distinct remote messages, not link rows: a fork copies its prefix's links.
	messages, err := d.dbClient.RemoteSyncMessage.Query().
		Where(remotesyncmessage.HasLinkWith(remotesyncchat.HasSyncWith(remotesync.ID(syncID)))).
		Unique(true).
		Select(remotesyncmessage.FieldRemoteMessageID).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count remote sync messages: %w", err)
	}
	totals.Messages = messages
	return totals, nil
}

// UpsertRemoteSync saves the user's sync settings, encrypting the token. The first save needs a
// token; later saves keep the stored one when in.Token is nil. Any save clears needs_attention,
// so a fixed token (or a hosted-side scope change) is retried at once.
func (d *Datastore) UpsertRemoteSync(ctx context.Context, userID uuid.UUID, in models.RemoteSyncUpsert) error {
	var enc, hint string
	if in.Token != nil {
		token := strings.TrimSpace(*in.Token)
		if token == "" {
			return ErrRemoteSyncTokenRequired
		}
		var err error
		if enc, err = d.encryptTokenForWrite(token); err != nil {
			return err
		}
		hint = tokenHint(token)
	}

	row, err := d.ownedRemoteSync(ctx, userID)
	if errors.Is(err, ErrRemoteSyncNotFound) {
		if enc == "" {
			return ErrRemoteSyncTokenRequired
		}
		create := d.dbClient.RemoteSync.Create().
			SetOwnerID(userID).
			SetBaseURL(in.BaseURL).
			SetToken(enc).
			SetTokenHint(hint)
		if in.Enabled != nil {
			create.SetEnabled(*in.Enabled)
		}
		if _, err := create.Save(ctx); err != nil {
			return fmt.Errorf("create remote sync: %w", err)
		}
		return nil
	}
	if err != nil {
		return err
	}

	upd := d.dbClient.RemoteSync.UpdateOneID(row.ID).
		SetBaseURL(in.BaseURL).
		SetStatus(remotesync.StatusPending).
		ClearLastError()
	if enc != "" {
		upd.SetToken(enc).SetTokenHint(hint)
	}
	if in.Enabled != nil {
		upd.SetEnabled(*in.Enabled)
	}
	if err := upd.Exec(ctx); err != nil {
		return fmt.Errorf("update remote sync: %w", err)
	}
	return nil
}

// DeleteRemoteSync stops the user's sync and forgets its token and links. Local threads stay;
// a later sync re-adopts them by their import hash instead of copying them again.
func (d *Datastore) DeleteRemoteSync(ctx context.Context, userID uuid.UUID) error {
	n, err := d.dbClient.RemoteSync.Delete().
		Where(remotesync.HasOwnerWith(user.ID(userID))).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete remote sync: %w", err)
	}
	if n == 0 {
		return ErrRemoteSyncNotFound
	}
	return nil
}

// GetRemoteSyncAccount returns the user's sync with its token decrypted, for the syncer.
func (d *Datastore) GetRemoteSyncAccount(ctx context.Context, userID uuid.UUID) (*models.RemoteSyncAccount, error) {
	row, err := d.ownedRemoteSync(ctx, userID)
	if err != nil {
		return nil, err
	}
	token, err := d.decryptTokenForRead(row.Token)
	if err != nil {
		return nil, fmt.Errorf("decrypt remote sync token: %w", err)
	}
	return &models.RemoteSyncAccount{
		ID:        row.ID,
		UserID:    userID,
		BaseURL:   row.BaseURL,
		Token:     token,
		Enabled:   row.Enabled,
		Status:    models.RemoteSyncStatus(row.Status),
		LastRunAt: row.LastRunAt,
		FullWalk: row.LastSuccessAt == nil ||
			models.RemoteSyncRunStatsFromMap(row.LastRun).ThreadsDeferred > 0,
	}, nil
}

// ExtendRemoteSyncLease keeps a long run's lease from expiring under it.
func (d *Datastore) ExtendRemoteSyncLease(ctx context.Context, userID uuid.UUID, leaseUntil time.Time) error {
	_, err := d.dbClient.RemoteSync.Update().
		Where(remotesync.HasOwnerWith(user.ID(userID)), remotesync.LeaseUntilNotNil()).
		SetLeaseUntil(leaseUntil).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("extend remote sync lease: %w", err)
	}
	return nil
}

// ListDueRemoteSyncs returns the accounts whose enabled sync last ran before lastRunBefore (or
// never), is not waiting on its owner (needs_attention) and is not held by a run.
func (d *Datastore) ListDueRemoteSyncs(ctx context.Context, lastRunBefore time.Time) ([]models.RemoteSyncDue, error) {
	now := time.Now()
	rows, err := d.dbClient.RemoteSync.Query().
		Where(
			remotesync.Enabled(true),
			remotesync.StatusNEQ(remotesync.StatusNeedsAttention),
			remotesync.Or(remotesync.LastRunAtIsNil(), remotesync.LastRunAtLT(lastRunBefore)),
			remotesync.Or(remotesync.LeaseUntilIsNil(), remotesync.LeaseUntilLT(now)),
		).
		WithOwner().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list due remote syncs: %w", err)
	}
	out := make([]models.RemoteSyncDue, 0, len(rows))
	for _, r := range rows {
		if r.Edges.Owner != nil {
			out = append(out, models.RemoteSyncDue{UserID: r.Edges.Owner.ID, LastRunAt: r.LastRunAt})
		}
	}
	return out, nil
}

// ClaimRemoteSync takes the user's sync lease until leaseUntil, and reports whether this caller
// got it. The conditional update is the whole lock: of two instances (or a tick and "sync now")
// trying at once, exactly one wins, and a run that died leaves a lease that simply expires.
func (d *Datastore) ClaimRemoteSync(ctx context.Context, userID uuid.UUID, leaseUntil time.Time) (bool, error) {
	n, err := d.dbClient.RemoteSync.Update().
		Where(
			remotesync.HasOwnerWith(user.ID(userID)),
			remotesync.Or(remotesync.LeaseUntilIsNil(), remotesync.LeaseUntilLT(time.Now())),
		).
		SetLeaseUntil(leaseUntil).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("claim remote sync: %w", err)
	}
	return n == 1, nil
}

// FinishRemoteSyncRun records a run's outcome and releases the lease.
func (d *Datastore) FinishRemoteSyncRun(ctx context.Context, userID uuid.UUID, result models.RemoteSyncRunResult) error {
	now := time.Now()
	upd := d.dbClient.RemoteSync.Update().
		Where(remotesync.HasOwnerWith(user.ID(userID))).
		SetStatus(remotesync.Status(result.Status)).
		SetLastRunAt(now).
		SetLastRun(result.Stats.AsMap()).
		ClearLeaseUntil()
	if result.Error != nil {
		upd.SetLastError(*result.Error)
	} else {
		upd.ClearLastError()
	}
	if result.Status == models.RemoteSyncStatusOK {
		upd.SetLastSuccessAt(now)
	}
	if _, err := upd.Save(ctx); err != nil {
		return fmt.Errorf("finish remote sync run: %w", err)
	}
	return nil
}

// ListRemoteSyncChatLinks returns the user's thread links keyed by remote thread id.
func (d *Datastore) ListRemoteSyncChatLinks(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]models.RemoteSyncChatLink, error) {
	rows, err := d.dbClient.RemoteSyncChat.Query().
		Where(remotesyncchat.HasSyncWith(remotesync.HasOwnerWith(user.ID(userID)))).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list remote sync links: %w", err)
	}
	out := make(map[uuid.UUID]models.RemoteSyncChatLink, len(rows))
	for _, r := range rows {
		out[r.RemoteChatID] = remoteSyncChatLinkModel(r)
	}
	return out, nil
}

func (d *Datastore) ownedRemoteSync(ctx context.Context, userID uuid.UUID) (*ent.RemoteSync, error) {
	row, err := d.dbClient.RemoteSync.Query().
		Where(remotesync.HasOwnerWith(user.ID(userID))).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrRemoteSyncNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get remote sync: %w", err)
	}
	return row, nil
}

func remoteSyncChatLinkModel(r *ent.RemoteSyncChat) models.RemoteSyncChatLink {
	return models.RemoteSyncChatLink{
		ID:                    r.ID,
		RemoteChatID:          r.RemoteChatID,
		ChatID:                r.ChatID,
		RemoteName:            r.RemoteName,
		RemotePersonalityName: r.RemotePersonalityName,
		RemoteLastMessageTime: r.RemoteLastMessageTime,
		RemoteHeadSentAt:      r.RemoteHeadSentAt,
		Forks:                 r.Forks,
		State:                 models.RemoteSyncLinkState(r.State),
	}
}

// tokenHint is the last four characters of a token, for display only.
func tokenHint(token string) string {
	r := []rune(token)
	if len(r) <= 4 {
		return ""
	}
	return string(r[len(r)-4:])
}

// unmatchedRemoteCopies maps the user's tracked copies that have no persona to the name of their
// remote thread's persona.
func (d *Datastore) unmatchedRemoteCopies(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]string, error) {
	links, err := d.dbClient.RemoteSyncChat.Query().
		Where(
			remotesyncchat.HasSyncWith(remotesync.HasOwnerWith(user.ID(userID))),
			remotesyncchat.StateEQ(remotesyncchat.StateActive),
			remotesyncchat.RemotePersonalityNameNEQ(""),
		).
		Select(remotesyncchat.FieldChatID, remotesyncchat.FieldRemotePersonalityName).
		All(ctx)
	if err != nil || len(links) == 0 {
		return nil, err
	}
	names := make(map[uuid.UUID]string, len(links))
	ids := make([]uuid.UUID, 0, len(links))
	for _, l := range links {
		names[l.ChatID] = l.RemotePersonalityName
		ids = append(ids, l.ChatID)
	}
	bare, err := d.dbClient.Chat.Query().
		Where(entchat.IDIn(ids...), entchat.HasOwnerWith(user.ID(userID)), entchat.Not(entchat.HasPersonality())).
		IDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("find copies without a persona: %w", err)
	}
	out := make(map[uuid.UUID]string, len(bare))
	for _, id := range bare {
		out[id] = names[id]
	}
	return out, nil
}

// RematchRemoteSyncPersonas gives each copy that has no persona the local persona named like its
// remote one, once such a persona exists (created here, or brought by an account import), and
// reports how many copies it changed. A copy is matched when it is made; this catches the ones
// whose persona arrived later. It never replaces a persona someone chose.
func (d *Datastore) RematchRemoteSyncPersonas(ctx context.Context, userID uuid.UUID) (int, error) {
	unmatched, err := d.unmatchedRemoteCopies(ctx, userID)
	if err != nil || len(unmatched) == 0 {
		return 0, err
	}
	byName := map[string][]uuid.UUID{}
	for chatID, name := range unmatched {
		key := strings.ToLower(strings.TrimSpace(name))
		byName[key] = append(byName[key], chatID)
	}
	changed := 0
	for name, chatIDs := range byName {
		pid, err := matchPersonalityByName(ctx, d.dbClient.Personality, userID, name)
		if err != nil {
			return changed, err
		}
		if pid == nil {
			continue
		}
		n, err := d.dbClient.Chat.Update().
			Where(entchat.IDIn(chatIDs...), entchat.HasOwnerWith(user.ID(userID)), entchat.Not(entchat.HasPersonality())).
			SetPersonalityID(*pid).
			Save(ctx)
		if err != nil {
			return changed, fmt.Errorf("assign persona to copies: %w", err)
		}
		changed += n
	}
	return changed, nil
}
