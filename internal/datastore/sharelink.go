package datastore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/shareban"
	"github.com/theimaginaryfoundation/what-iff/ent/sharelink"
	"github.com/theimaginaryfoundation/what-iff/ent/shareunlockattempt"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Share links (ADR 0x051): a public link to one of the user's files, on the file's original row,
// deleted with the file. The passcode is kept encrypted with the token key, so the owner can read
// it again; a server without that key makes only links without one.

// ErrSharePasscodeUnavailable is returned when a link would need a passcode and the server has no
// key to keep one with (TOKEN_ENCRYPTION_SECRET).
var ErrSharePasscodeUnavailable = errors.New("this server cannot keep share passcodes: set TOKEN_ENCRYPTION_SECRET, or share without a passcode")

// passcodeAlphabet is what a share passcode is made of: 62 letters and digits, case-sensitive.
const passcodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// SharePasscodeLength is how many characters a share passcode has (71 bits).
const SharePasscodeLength = 12

// NewSharePasscode makes a passcode: SharePasscodeLength characters of passcodeAlphabet, each
// drawn without bias from a cryptographic source.
func NewSharePasscode() (string, error) {
	out := make([]byte, SharePasscodeLength)
	n := big.NewInt(int64(len(passcodeAlphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", err
		}
		out[i] = passcodeAlphabet[v.Int64()]
	}
	return string(out), nil
}

// newShareKey is a link's key: 128 random bits, base64url without padding (22 characters).
func newShareKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// shareableFile is the original row of the user's file id (a chat copy shares its original), or
// an error when it is not the user's or cannot be shared.
func (d *Datastore) shareableFile(ctx context.Context, userID, id uuid.UUID) (*ent.FileAttachment, error) {
	original, err := d.CommentFileID(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	row, err := d.dbClient.FileAttachment.Query().
		Where(entfileattachment.ID(original), entfileattachment.HasOwnerWith(user.ID(userID))).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrFileAttachmentNotFound
	}
	if err != nil {
		return nil, err
	}
	if row.Origin == entfileattachment.OriginSkill {
		return nil, models.ErrShareNotShareable
	}
	return row, nil
}

// ShareLinkForFile is the share link of the user's file (any row of it), with its passcode.
func (d *Datastore) ShareLinkForFile(ctx context.Context, userID, fileID uuid.UUID) (*models.ShareLink, error) {
	file, err := d.shareableFile(ctx, userID, fileID)
	if err != nil {
		return nil, err
	}
	link, err := d.dbClient.ShareLink.Query().Where(sharelink.FileAttachmentID(file.ID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, models.ErrShareLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	return d.shareLinkModel(link, file), nil
}

// CreateShareLink shares the user's file (any row of it), with a new passcode when withPasscode;
// a file shared already keeps its link as it is. created says whether it made one.
func (d *Datastore) CreateShareLink(ctx context.Context, userID, fileID uuid.UUID, withPasscode bool) (link *models.ShareLink, created bool, err error) {
	file, err := d.shareableFile(ctx, userID, fileID)
	if err != nil {
		return nil, false, err
	}
	if existing, err := d.dbClient.ShareLink.Query().Where(sharelink.FileAttachmentID(file.ID)).Only(ctx); err == nil {
		return d.shareLinkModel(existing, file), false, nil
	} else if !ent.IsNotFound(err) {
		return nil, false, err
	}
	ciphertext := ""
	if withPasscode {
		if ciphertext, err = d.newPasscodeCiphertext(); err != nil {
			return nil, false, err
		}
	}
	key, err := newShareKey()
	if err != nil {
		return nil, false, err
	}
	row, err := d.dbClient.ShareLink.Create().
		SetKey(key).
		SetFileAttachmentID(file.ID).
		SetPasscodeCiphertext(ciphertext).
		Save(ctx)
	if ent.IsConstraintError(err) {
		// Shared at the same moment by another request: that link is the file's.
		link, err := d.ShareLinkForFile(ctx, userID, file.ID)
		return link, false, err
	}
	if err != nil {
		return nil, false, err
	}
	return d.shareLinkModel(row, file), true, nil
}

// UpdateShareLink changes the share link of the user's file: requirePasscode (when not nil) turns
// the passcode on (with a new one) or off; regenerate makes a new one for a link that needs one.
// A new passcode starts a new generation, so every visitor who unlocked the link is signed out.
func (d *Datastore) UpdateShareLink(ctx context.Context, userID, fileID uuid.UUID, requirePasscode *bool, regenerate bool) (*models.ShareLink, error) {
	file, err := d.shareableFile(ctx, userID, fileID)
	if err != nil {
		return nil, err
	}
	row, err := d.dbClient.ShareLink.Query().Where(sharelink.FileAttachmentID(file.ID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, models.ErrShareLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	has := row.PasscodeCiphertext != ""
	want := has
	if requirePasscode != nil {
		want = *requirePasscode
	}
	update := row.Update()
	switch {
	case want && (!has || regenerate):
		ciphertext, err := d.newPasscodeCiphertext()
		if err != nil {
			return nil, err
		}
		update.SetPasscodeCiphertext(ciphertext).AddPasscodeGeneration(1)
	case !want && has:
		update.SetPasscodeCiphertext("")
	default:
		return d.shareLinkModel(row, file), nil
	}
	row, err = update.Save(ctx)
	if err != nil {
		return nil, err
	}
	return d.shareLinkModel(row, file), nil
}

// DeleteShareLink stops sharing the user's file (any row of it). Deleting one that is not there
// changes nothing.
func (d *Datastore) DeleteShareLink(ctx context.Context, userID, fileID uuid.UUID) error {
	file, err := d.shareableFile(ctx, userID, fileID)
	if err != nil {
		return err
	}
	_, err = d.dbClient.ShareLink.Delete().Where(sharelink.FileAttachmentID(file.ID)).Exec(ctx)
	return err
}

// ListShareLinks is every share link of the user's, newest first, with their passcodes.
func (d *Datastore) ListShareLinks(ctx context.Context, userID uuid.UUID) ([]models.ShareLink, error) {
	rows, err := d.dbClient.ShareLink.Query().
		Where(sharelink.HasFileAttachmentWith(entfileattachment.HasOwnerWith(user.ID(userID)))).
		WithFileAttachment().
		Order(ent.Desc(sharelink.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.ShareLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, *d.shareLinkModel(r, r.Edges.FileAttachment))
	}
	return out, nil
}

// shareLinkModel is a link as its owner sees it. It cannot fail: a passcode that cannot be
// decrypted is flagged on the link (PasscodeUnreadable), so one bad row never hides the others.
func (d *Datastore) shareLinkModel(row *ent.ShareLink, file *ent.FileAttachment) *models.ShareLink {
	link := &models.ShareLink{Key: row.Key, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, PasscodeRequired: row.PasscodeCiphertext != ""}
	if file != nil {
		link.FileID, link.FileName, link.Folder, link.FileType = file.ID, file.Name, file.Folder, file.FileType
		link.WebApp = models.SharesAsWebApp(&models.FileAttachment{Name: file.Name, FileType: file.FileType, Folder: file.Folder})
	}
	if link.PasscodeRequired {
		passcode, err := d.decryptTokenForRead(row.PasscodeCiphertext)
		if err != nil {
			// One unreadable passcode must not keep the owner from their other links, or from
			// making a new passcode for this one.
			d.logger.Warn("share link passcode could not be decrypted", zap.Error(err))
			link.PasscodeUnreadable = true
			return link
		}
		link.Passcode = passcode
	}
	return link
}

// ShareScope is what sharing the user's file (any row of it) shares: the file, or its folder as a
// web app.
func (d *Datastore) ShareScope(ctx context.Context, userID, fileID uuid.UUID) (models.ShareScope, error) {
	file, err := d.shareableFile(ctx, userID, fileID)
	if err != nil {
		return models.ShareScope{}, err
	}
	scope := models.ShareScope{FileName: file.Name}
	if models.SharesAsWebApp(&models.FileAttachment{Name: file.Name, FileType: file.FileType, Folder: file.Folder}) {
		scope.WebApp, scope.Folder = true, file.Folder
	}
	return scope, nil
}

func (d *Datastore) newPasscodeCiphertext() (string, error) {
	if d.tokenCrypto == nil {
		return "", ErrSharePasscodeUnavailable
	}
	passcode, err := NewSharePasscode()
	if err != nil {
		return "", err
	}
	return d.tokenCrypto.Encrypt(passcode)
}

// SharedItemByKey is what the share link with this key opens: its file and owner. Visitors reach
// it without signing in (ADR 0x051 §5).
func (d *Datastore) SharedItemByKey(ctx context.Context, key string) (*models.SharedItem, error) {
	if key == "" {
		return nil, models.ErrShareLinkNotFound
	}
	row, err := d.dbClient.ShareLink.Query().
		Where(sharelink.Key(key)).
		WithFileAttachment(func(q *ent.FileAttachmentQuery) {
			q.Select(sharedFileFields...).WithOwner(func(u *ent.UserQuery) { u.Select(user.FieldID) })
		}).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, models.ErrShareLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	file := row.Edges.FileAttachment
	if file == nil || file.Edges.Owner == nil {
		return nil, models.ErrShareLinkNotFound
	}
	return &models.SharedItem{
		Key:                row.Key,
		OwnerID:            file.Edges.Owner.ID,
		File:               toFileAttachmentModel(file),
		PasscodeCiphertext: row.PasscodeCiphertext,
		PasscodeGeneration: row.PasscodeGeneration,
	}, nil
}

// SharePasscode is a shared item's passcode, decrypted; empty when it needs none.
func (d *Datastore) SharePasscode(item *models.SharedItem) (string, error) {
	return d.decryptTokenForRead(item.PasscodeCiphertext)
}

// sharedFileFields are the columns a visitor's reads need: never a file's extracted text
// (file_content), which the public routes would otherwise load for every file they list.
var sharedFileFields = []string{
	entfileattachment.FieldID,
	entfileattachment.FieldName,
	entfileattachment.FieldFileType,
	entfileattachment.FieldFolder,
	entfileattachment.FieldS3Key,
	entfileattachment.FieldSizeBytes,
	entfileattachment.FieldContentVersion,
	entfileattachment.FieldCreatedAt,
	entfileattachment.FieldOrigin,
}

// MaxSharedProjectFiles bounds how many files a shared web app lists.
const MaxSharedProjectFiles = 2000

// SharedProjectFiles is a shared web app's files (ADR 0x051 §7): the owner's gallery files in
// root and the folders inside it, and nothing else (no skill's files, no chat copies). root must
// not be the top level.
func (d *Datastore) SharedProjectFiles(ctx context.Context, ownerID uuid.UUID, root string) ([]models.FileAttachment, error) {
	if root == "" {
		return nil, models.ErrInvalidFolder
	}
	rows, err := d.dbClient.FileAttachment.Query().
		Where(galleryMutableFiles(ownerID, models.GalleryKindAll)...).
		Where(entfileattachment.Or(
			entfileattachment.FolderEQ(root),
			entfileattachment.FolderHasPrefix(root+"/"),
		)).
		Order(ent.Asc(entfileattachment.FieldFolder), ent.Asc(entfileattachment.FieldName), ent.Desc(entfileattachment.FieldCreatedAt)).
		Limit(MaxSharedProjectFiles).
		Select(sharedFileFields...).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.FileAttachment, 0, len(rows))
	for _, r := range rows {
		out = append(out, *toFileAttachmentModel(r))
	}
	return out, nil
}

// SharedProjectFile is the file named name in folder of a shared web app's owner, where folder is
// root or a folder inside it (the caller has resolved and checked the path); the newest of that
// name when there are several.
func (d *Datastore) SharedProjectFile(ctx context.Context, ownerID uuid.UUID, root, folder, name string) (*models.FileAttachment, error) {
	if root == "" || !models.FolderIsWithin(folder, root) || name == "" {
		return nil, ErrFileAttachmentNotFound
	}
	row, err := d.dbClient.FileAttachment.Query().
		Where(galleryMutableFiles(ownerID, models.GalleryKindAll)...).
		Where(entfileattachment.FolderEQ(folder), entfileattachment.Name(name)).
		Order(ent.Desc(entfileattachment.FieldCreatedAt)).
		Select(sharedFileFields...).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrFileAttachmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return toFileAttachmentModel(row), nil
}

// --- Unlock attempts and bans (ADR 0x051 §4) ------------------------------------------------

// RecordShareUnlockAttempt records a try at a passcode by an address (a keyed hash), as a failure
// until ClearShareUnlockFailure says otherwise, and deletes attempts, and bans that ended, older
// than keep: the tables hold at most a day of tries and bans.
func (d *Datastore) RecordShareUnlockAttempt(ctx context.Context, addressKey string, at time.Time, keep time.Duration) (uuid.UUID, error) {
	row, err := d.dbClient.ShareUnlockAttempt.Create().SetAddressKey(addressKey).SetFailed(true).SetAt(at).Save(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := d.dbClient.ShareUnlockAttempt.Delete().Where(shareunlockattempt.AtLT(at.Add(-keep))).Exec(ctx); err != nil {
		return row.ID, err
	}
	_, err = d.dbClient.ShareBan.Delete().Where(shareban.UntilLT(at.Add(-keep))).Exec(ctx)
	return row.ID, err
}

// ClearShareUnlockFailure records that a try was no wrong guess (the passcode was right, or it
// was refused unchecked).
func (d *Datastore) ClearShareUnlockFailure(ctx context.Context, id uuid.UUID) error {
	return d.dbClient.ShareUnlockAttempt.UpdateOneID(id).SetFailed(false).Exec(ctx)
}

// DiscardShareUnlockAttempt deletes a try that was refused unchecked (over the rate limit).
func (d *Datastore) DiscardShareUnlockAttempt(ctx context.Context, id uuid.UUID) error {
	return d.dbClient.ShareUnlockAttempt.DeleteOneID(id).Exec(ctx)
}

// ShareUnlockAttempts counts an address's tries since a time (every try, or only the failed ones)
// and says when the oldest of them was.
func (d *Datastore) ShareUnlockAttempts(ctx context.Context, addressKey string, since time.Time, failedOnly bool) (int, time.Time, error) {
	q := d.dbClient.ShareUnlockAttempt.Query().
		Where(shareunlockattempt.AddressKey(addressKey), shareunlockattempt.AtGTE(since))
	if failedOnly {
		q = q.Where(shareunlockattempt.Failed(true))
	}
	n, err := q.Clone().Count(ctx)
	if err != nil || n == 0 {
		return n, time.Time{}, err
	}
	oldest, err := q.Order(ent.Asc(shareunlockattempt.FieldAt)).First(ctx)
	if err != nil {
		return n, time.Time{}, err
	}
	return n, oldest.At, nil
}

// ShareBan is an address's ban: until when, and how many bans in a row it has had; zero values
// for an address never banned.
func (d *Datastore) ShareBan(ctx context.Context, addressKey string) (until time.Time, strikes int, err error) {
	row, err := d.dbClient.ShareBan.Query().Where(shareban.AddressKey(addressKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return time.Time{}, 0, nil
	}
	if err != nil {
		return time.Time{}, 0, err
	}
	return row.Until, row.Strikes, nil
}

// SetShareBan bans an address until a time, with its count of bans in a row.
func (d *Datastore) SetShareBan(ctx context.Context, addressKey string, until time.Time, strikes int) error {
	return d.dbClient.ShareBan.Create().
		SetAddressKey(addressKey).
		SetUntil(until).
		SetStrikes(strikes).
		OnConflictColumns(shareban.FieldAddressKey).
		UpdateNewValues().
		Exec(ctx)
}
