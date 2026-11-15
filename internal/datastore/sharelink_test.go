package datastore

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestNewSharePasscode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		p, err := NewSharePasscode()
		require.NoError(t, err)
		require.Len(t, p, SharePasscodeLength)
		require.Equal(t, -1, strings.IndexFunc(p, func(r rune) bool { return r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r)) }))
		require.False(t, seen[p])
		seen[p] = true
	}
}

func TestShareLinks_OwnerLifecycle(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID, other := createFATestUser(t, ds), createFATestUser(t, ds)
	file, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "index.html", FileType: "text/html", S3Key: "users/u/index.html", Folder: "site"})
	require.NoError(t, err)
	copied, err := ds.CreateFileAttachmentReference(ctx, userID, file.ID)
	require.NoError(t, err)

	// Without the token key, only links without a passcode.
	ds.tokenCrypto = nil
	_, _, err = ds.CreateShareLink(ctx, userID, file.ID, true)
	require.ErrorIs(t, err, ErrSharePasscodeUnavailable)
	crypto, err := newTokenCrypto(strings.Repeat("k", MinTokenEncryptionSecretLen))
	require.NoError(t, err)
	ds.tokenCrypto = crypto

	link, created, err := ds.CreateShareLink(ctx, userID, copied.ID, true)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, file.ID, link.FileID, "a chat copy shares its original")
	require.Len(t, link.Key, 22)
	require.True(t, link.PasscodeRequired)
	require.Len(t, link.Passcode, SharePasscodeLength)
	require.True(t, link.WebApp)
	require.Equal(t, "site", link.Folder)

	again, created, err := ds.CreateShareLink(ctx, userID, file.ID, false)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, link.Key, again.Key, "sharing again keeps the link")
	require.Equal(t, link.Passcode, again.Passcode)

	_, err = ds.ShareLinkForFile(ctx, other, file.ID)
	require.ErrorIs(t, err, ErrFileAttachmentNotFound, "someone else's file")

	item, err := ds.SharedItemByKey(ctx, link.Key)
	require.NoError(t, err)
	require.Equal(t, userID, item.OwnerID)
	require.Equal(t, 1, item.PasscodeGeneration)
	passcode, err := ds.SharePasscode(item)
	require.NoError(t, err)
	require.Equal(t, link.Passcode, passcode)
	require.NotContains(t, item.PasscodeCiphertext, passcode, "stored encrypted")

	// A new passcode is a new generation; turning it off and on again too.
	renewed, err := ds.UpdateShareLink(ctx, userID, file.ID, nil, true)
	require.NoError(t, err)
	require.NotEqual(t, link.Passcode, renewed.Passcode)
	off := false
	open, err := ds.UpdateShareLink(ctx, userID, file.ID, &off, false)
	require.NoError(t, err)
	require.False(t, open.PasscodeRequired)
	require.Empty(t, open.Passcode)
	on := true
	_, err = ds.UpdateShareLink(ctx, userID, file.ID, &on, false)
	require.NoError(t, err)
	item, err = ds.SharedItemByKey(ctx, link.Key)
	require.NoError(t, err)
	require.Equal(t, 3, item.PasscodeGeneration)

	links, err := ds.ListShareLinks(ctx, userID)
	require.NoError(t, err)
	require.Len(t, links, 1)
	links, err = ds.ListShareLinks(ctx, other)
	require.NoError(t, err)
	require.Empty(t, links)

	// Stopping sharing deletes the link; sharing again makes a new one.
	require.NoError(t, ds.DeleteShareLink(ctx, userID, file.ID))
	_, err = ds.SharedItemByKey(ctx, link.Key)
	require.ErrorIs(t, err, models.ErrShareLinkNotFound)
	fresh, _, err := ds.CreateShareLink(ctx, userID, file.ID, false)
	require.NoError(t, err)
	require.NotEqual(t, link.Key, fresh.Key)

	// Deleting the file deletes its link.
	require.NoError(t, ds.dbClient.FileAttachment.DeleteOneID(copied.ID).Exec(ctx))
	require.NoError(t, ds.dbClient.FileAttachment.DeleteOneID(file.ID).Exec(ctx))
	_, err = ds.SharedItemByKey(ctx, fresh.Key)
	require.ErrorIs(t, err, models.ErrShareLinkNotFound)
}

func TestShareLinks_SkillFilesAreNotShared(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	file, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "SKILL.md", FileType: "text/markdown", S3Key: "k"})
	require.NoError(t, err)
	require.NoError(t, ds.dbClient.FileAttachment.UpdateOneID(file.ID).SetOrigin(entfileattachment.OriginSkill).Exec(ctx))
	_, _, err = ds.CreateShareLink(ctx, userID, file.ID, false)
	require.ErrorIs(t, err, models.ErrShareNotShareable)
}

func TestSharedProjectFiles_AreTheFolderAndBelowOnly(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	owner, other := createFATestUser(t, ds), createFATestUser(t, ds)
	add := func(user uuid.UUID, folder, name string) uuid.UUID {
		f, err := ds.CreateFileAttachment(ctx, user, models.FileAttachment{Name: name, FileType: "text/plain", S3Key: "k/" + name, Folder: folder})
		require.NoError(t, err)
		return f.ID
	}
	page := add(owner, "site", "index.html")
	css := add(owner, "site/css", "app.css")
	add(owner, "sites", "lookalike.txt")
	add(owner, "", "top.txt")
	add(owner, "other", "secret.txt")
	add(other, "site", "theirs.js")
	_, err := ds.CreateFileAttachmentReference(ctx, owner, page)
	require.NoError(t, err)

	files, err := ds.SharedProjectFiles(ctx, owner, "site")
	require.NoError(t, err)
	var ids []uuid.UUID
	for _, f := range files {
		ids = append(ids, f.ID)
	}
	require.ElementsMatch(t, []uuid.UUID{page, css}, ids)

	f, err := ds.SharedProjectFile(ctx, owner, "site", "site/css", "app.css")
	require.NoError(t, err)
	require.Equal(t, css, f.ID)
	_, err = ds.SharedProjectFile(ctx, owner, "site", "other", "secret.txt")
	require.ErrorIs(t, err, ErrFileAttachmentNotFound, "a folder outside the root is refused even when asked directly")
	_, err = ds.SharedProjectFile(ctx, other, "site", "site", "index.html")
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
	_, err = ds.SharedProjectFiles(ctx, owner, "")
	require.Error(t, err, "the top level is no project")
}

func TestShareUnlockAttemptsAndBans(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	day := 24 * time.Hour

	// A try counts as failed until cleared.
	_, err := ds.RecordShareUnlockAttempt(ctx, "a", now.Add(-2*time.Hour), day)
	require.NoError(t, err)
	right, err := ds.RecordShareUnlockAttempt(ctx, "a", now.Add(-30*time.Second), day)
	require.NoError(t, err)
	require.NoError(t, ds.ClearShareUnlockFailure(ctx, right))
	n, oldest, err := ds.ShareUnlockAttempts(ctx, "a", now.Add(-time.Minute), false)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.True(t, oldest.Equal(now.Add(-30*time.Second)))
	n, _, err = ds.ShareUnlockAttempts(ctx, "a", now.Add(-day), true)
	require.NoError(t, err)
	require.Equal(t, 1, n, "the right one is no failure")

	until, strikes, err := ds.ShareBan(ctx, "a")
	require.NoError(t, err)
	require.True(t, until.IsZero())
	require.Zero(t, strikes)
	require.NoError(t, ds.SetShareBan(ctx, "a", now.Add(time.Hour), 1))
	require.NoError(t, ds.SetShareBan(ctx, "a", now.Add(day), 2))
	until, strikes, err = ds.ShareBan(ctx, "a")
	require.NoError(t, err)
	require.True(t, until.Equal(now.Add(day)))
	require.Equal(t, 2, strikes)

	// Recording a try deletes tries, and bans that ended, older than keep.
	require.NoError(t, ds.SetShareBan(ctx, "old", now.Add(-2*day), 1))
	_, err = ds.RecordShareUnlockAttempt(ctx, "b", now, time.Hour)
	require.NoError(t, err)
	n, _, err = ds.ShareUnlockAttempts(ctx, "a", now.Add(-day), true)
	require.NoError(t, err)
	require.Zero(t, n, "the try two hours old is gone")
	until, _, err = ds.ShareBan(ctx, "old")
	require.NoError(t, err)
	require.True(t, until.IsZero(), "a ban that ended two days ago is gone")
	until, _, err = ds.ShareBan(ctx, "a")
	require.NoError(t, err)
	require.False(t, until.IsZero(), "a ban still running stays")
}
