package filecomments

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/filesave"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

// Comments in Word, PowerPoint and Excel files (ADR 0x046) are part of the file, unlike a text
// file's, so a change to them is a new version of the file: the format (officecomments) changes
// the bytes and the file is saved the way the file workspace saves it, over the version read.

// Saver saves a file's new content; *filesave.Service satisfies it.
type Saver interface {
	Replace(ctx context.Context, userID uuid.UUID, req filesave.Request) (*models.FileAttachment, error)
}

// Office reads and changes the comments inside Office files.
type Office struct {
	store   Store
	files   storage.FileStore
	saver   Saver
	formats map[string]officecomments.Format
}

// NewOffice returns an Office over formats by file extension (".docx", ".pptx", ".xlsx").
func NewOffice(store Store, files storage.FileStore, saver Saver, formats map[string]officecomments.Format) *Office {
	return &Office{store: store, files: files, saver: saver, formats: formats}
}

// FormatOf is the format of an Office file, or nil for any other file.
func (o *Office) FormatOf(file *models.FileAttachment) officecomments.Format {
	if o == nil {
		return nil
	}
	return o.formats[strings.ToLower(path.Ext(file.Name))]
}

func (o *Office) read(ctx context.Context, userID uuid.UUID, file *models.FileAttachment) ([]byte, error) {
	return storage.ReadAttachment(ctx, o.files, storage.AttachmentObject{
		UserID: userID, ID: file.ID, S3Key: file.S3Key, Name: file.Name, FileType: file.FileType,
		ChatID: file.ChatID, PersonalityID: file.PersonalityID,
	})
}

// List is the threads in the file id.
func (o *Office) List(ctx context.Context, userID, id uuid.UUID) ([]officecomments.Thread, error) {
	file, err := o.store.GetFileAttachment(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	format := o.FormatOf(file)
	if format == nil {
		return nil, ErrNotText
	}
	data, err := o.read(ctx, userID, file)
	if err != nil {
		return nil, err
	}
	return format.List(data)
}

// Change applies change to the file's bytes and saves the result as its new version, from chatID
// when it is not nil. A save that finds the file changed since it was read starts again once.
// It returns what change returned with the bytes (a thread id).
func (o *Office) Change(ctx context.Context, userID uuid.UUID, chatID *uuid.UUID, id uuid.UUID, change func(officecomments.Format, []byte) ([]byte, string, error)) (string, error) {
	for attempt := 0; ; attempt++ {
		file, err := o.store.GetFileAttachment(ctx, userID, id)
		if err != nil {
			return "", err
		}
		format := o.FormatOf(file)
		if format == nil {
			return "", ErrNotText
		}
		data, err := o.read(ctx, userID, file)
		if err != nil {
			return "", err
		}
		next, result, err := change(format, data)
		if err != nil {
			return "", err
		}
		version := file.ContentVersion
		_, err = o.saver.Replace(ctx, userID, filesave.Request{ID: file.ID, Content: next, IfMatch: &version, ChatID: chatID})
		var conflict *filesave.ConflictError
		if errors.As(err, &conflict) && attempt == 0 {
			continue
		}
		if errors.As(err, &conflict) {
			return "", ErrStaleContent
		}
		return result, err
	}
}
