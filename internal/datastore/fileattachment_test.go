package datastore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createFileAttachmentTestSchema creates the file_attachments and personality_expressions
// tables that fileattachment.go's queries touch but that createMemoryImportTestSchema and
// createAccountBackupTestSchema do not already provide. file_attachments FKs to
// chat_messages (from createAccountBackupTestSchema), personalities and users (from
// createMemoryImportTestSchema), and moods (from createAccountBackupTestSchema).
// personality_expressions FKs to personalities and file_attachments.
//
// Must be composed after createMemoryImportTestSchema and createAccountBackupTestSchema via
// newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createFileAttachmentTestSchema).
func createFileAttachmentTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	statements := []string{
		`CREATE TABLE file_attachments (
			id uuid PRIMARY KEY,
			file_id text,
			name text NOT NULL,
			file_type text NOT NULL,
			description text,
			file_content text,
			chunk_status text,
			s3_key text,
			created_at datetime NOT NULL,
			chat_message_file_attachments uuid,
			mood_images uuid,
			personality_file_attachments uuid,
			user_file_attachments uuid NOT NULL
		)`,
		`CREATE TABLE personality_expressions (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			expression_key text NOT NULL,
			label text,
			personality_expressions uuid NOT NULL,
			personality_expression_image uuid
		)`,
	}

	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func newFileAttachmentTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	// alterChatsTableForAgentJobTests is needed because GetFileAttachment eager-loads
	// WithChatMessage(func(q) { q.WithChat() }), and ent's WithChat() selects every column
	// on the real chats schema (including source/import_hash/rehydration_state), which
	// createMemoryImportTestSchema's minimal chats fixture doesn't have.
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createFileAttachmentTestSchema, alterChatsTableForAgentJobTests)
}

func createFATestUser(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.dbClient.User.Create().
		SetID(id).
		SetUsername("fa-" + id.String()[:8]).
		SetEmail("fa-" + id.String()[:8] + "@example.com").
		SetPasswordHash("hash").
		Save(context.Background())
	require.NoError(t, err)
	return id
}

func createFATestPersonality(t *testing.T, ds *Datastore, userID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	p, err := ds.dbClient.Personality.Create().
		SetName(name).
		SetSystemPrompt("system prompt").
		SetUserID(userID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	require.NoError(t, err)
	return p.ID
}

func createFATestModel(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	m, err := ds.dbClient.Model.Create().
		SetName("model-" + uuid.NewString()[:8]).
		SetDisplayName("Test Model").
		SetDescription("test model").
		Save(context.Background())
	require.NoError(t, err)
	return m.ID
}

func createFATestChat(t *testing.T, ds *Datastore, userID, modelID uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	c, err := ds.dbClient.Chat.Create().
		SetName("Chat").
		SetOwnerID(userID).
		SetModelID(modelID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	require.NoError(t, err)
	return c.ID
}

func createFATestChatMessage(t *testing.T, ds *Datastore, chatID uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	cm, err := ds.dbClient.ChatMessage.Create().
		SetChatID(chatID).
		SetMessage("hello").
		SetOrigin("User").
		SetSentAt(now).
		Save(context.Background())
	require.NoError(t, err)
	return cm.ID
}

func TestToFileAttachmentModel_Nil(t *testing.T) {
	require.Nil(t, toFileAttachmentModel(nil))
}

func TestToFileAttachmentModel_NoEdges(t *testing.T) {
	now := time.Now().UTC()
	e := &ent.FileAttachment{
		ID:        uuid.New(),
		Name:      "file.txt",
		FileType:  "text/plain",
		S3Key:     "some/key",
		CreatedAt: now,
	}
	model := toFileAttachmentModel(e)
	require.NotNil(t, model)
	require.Equal(t, uuid.Nil, model.UserID)
	require.Equal(t, "file.txt", model.Name)
	require.Nil(t, model.FileID)
	require.Nil(t, model.ChatMessageID)
	require.Nil(t, model.ChatID)
	require.Nil(t, model.PersonalityID)
}

func TestCreateFileAttachment_HappyPath(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	desc := "a description"

	got, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:        "report.pdf",
		FileType:    "application/pdf",
		Description: &desc,
		S3Key:       "users/u/report.pdf",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, userID, got.UserID)
	require.Equal(t, "report.pdf", got.Name)
	require.Equal(t, "application/pdf", got.FileType)
	require.NotNil(t, got.Description)
	require.Equal(t, desc, *got.Description)
	require.Equal(t, "users/u/report.pdf", got.S3Key)
	require.Nil(t, got.PersonalityID)
	require.Nil(t, got.ChatMessageID)
}

func TestCreateFileAttachment_UserNotFound(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()

	_, err := ds.CreateFileAttachment(context.Background(), uuid.New(), models.FileAttachment{
		Name:     "report.pdf",
		FileType: "application/pdf",
	})
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestCreateFileAttachment_WithChatMessageAndPersonality(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, userID, modelID)
	chatMessageID := createFATestChatMessage(t, ds, chatID)
	personalityID := createFATestPersonality(t, ds, userID, "Vix")

	got, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "image.png",
		FileType:      "image/png",
		ChatMessageID: &chatMessageID,
		PersonalityID: &personalityID,
	})
	require.NoError(t, err)
	require.NotNil(t, got.ChatMessageID)
	require.Equal(t, chatMessageID, *got.ChatMessageID)
	// CreateFileAttachment's post-create reload uses WithChatMessage() without a nested
	// WithChat(), so ChatID is never populated here (unlike GetFileAttachment, which does
	// eager-load the chat edge). This is the real, current behavior, not a test bug.
	require.Nil(t, got.ChatID)
	require.NotNil(t, got.PersonalityID)
	require.Equal(t, personalityID, *got.PersonalityID)
	require.Len(t, got.Personalities, 1)
	require.Equal(t, "Vix", got.Personalities[0].Name)
}

func TestGetFileAttachment_Found(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, userID, modelID)
	chatMessageID := createFATestChatMessage(t, ds, chatID)

	created, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "image.png",
		FileType:      "image/png",
		ChatMessageID: &chatMessageID,
	})
	require.NoError(t, err)

	got, err := ds.GetFileAttachment(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, userID, got.UserID)
	require.NotNil(t, got.ChatMessageID)
	require.Equal(t, chatMessageID, *got.ChatMessageID)
	// Unlike CreateFileAttachment, GetFileAttachment eager-loads WithChat() nested under
	// WithChatMessage(), so ChatID is populated here.
	require.NotNil(t, got.ChatID)
	require.Equal(t, chatID, *got.ChatID)
}

func TestGetFileAttachment_NotFound(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()

	userID := createFATestUser(t, ds)
	_, err := ds.GetFileAttachment(context.Background(), userID, uuid.New())
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestGetFileAttachment_WrongOwner(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createFATestUser(t, ds)
	otherUserID := createFATestUser(t, ds)

	created, err := ds.CreateFileAttachment(ctx, ownerID, models.FileAttachment{
		Name:     "report.pdf",
		FileType: "application/pdf",
	})
	require.NoError(t, err)

	_, err = ds.GetFileAttachment(ctx, otherUserID, created.ID)
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestListFileAttachments_UserScopingAndPagination(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	otherUserID := createFATestUser(t, ds)

	for i := 0; i < 3; i++ {
		_, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
			Name:     "file.txt",
			FileType: "text/plain",
		})
		require.NoError(t, err)
	}
	_, err := ds.CreateFileAttachment(ctx, otherUserID, models.FileAttachment{
		Name:     "other-file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	page1, err := ds.ListFileAttachments(ctx, userID, 1, 2, models.FileAttachmentFilters{})
	require.NoError(t, err)
	require.Equal(t, 3, page1.TotalCount)
	require.Equal(t, 1, page1.Page)
	require.Len(t, page1.Results, 2)

	page2, err := ds.ListFileAttachments(ctx, userID, 2, 2, models.FileAttachmentFilters{})
	require.NoError(t, err)
	require.Equal(t, 3, page2.TotalCount)
	require.Equal(t, 2, page2.Page)
	require.Len(t, page2.Results, 1)
}

func TestListFileAttachments_InvalidPageDefaults(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	for i := 0; i < 2; i++ {
		_, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
			Name:     "file.txt",
			FileType: "text/plain",
		})
		require.NoError(t, err)
	}

	// pageNum < 1 defaults to 1, pageSize < 1 defaults to 10.
	res, err := ds.ListFileAttachments(ctx, userID, 0, 0, models.FileAttachmentFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, res.Page)
	require.Len(t, res.Results, 2)
}

func TestListFileAttachments_NameFilter(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	_, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "budget-report.pdf",
		FileType: "application/pdf",
	})
	require.NoError(t, err)
	_, err = ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "vacation.png",
		FileType: "image/png",
	})
	require.NoError(t, err)

	search := "budget"
	res, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{Name: &search})
	require.NoError(t, err)
	require.Equal(t, 1, res.TotalCount)
	require.Len(t, res.Results, 1)
	got := res.Results[0].(*models.FileAttachment)
	require.Equal(t, "budget-report.pdf", got.Name)
}

func TestListFileAttachments_FileTypeFilter(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	_, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "doc.pdf",
		FileType: "application/pdf",
	})
	require.NoError(t, err)
	_, err = ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "pic.png",
		FileType: "image/png",
	})
	require.NoError(t, err)

	fileType := "image"
	res, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{FileType: &fileType})
	require.NoError(t, err)
	require.Equal(t, 1, res.TotalCount)
	got := res.Results[0].(*models.FileAttachment)
	require.Equal(t, "pic.png", got.Name)
}

func TestListFileAttachments_ChatMessageIDFilter(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, userID, modelID)
	chatMessageID := createFATestChatMessage(t, ds, chatID)
	otherChatMessageID := createFATestChatMessage(t, ds, chatID)

	_, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "attached.txt",
		FileType:      "text/plain",
		ChatMessageID: &chatMessageID,
	})
	require.NoError(t, err)
	_, err = ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "other.txt",
		FileType:      "text/plain",
		ChatMessageID: &otherChatMessageID,
	})
	require.NoError(t, err)

	res, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{ChatMessageID: &chatMessageID})
	require.NoError(t, err)
	require.Equal(t, 1, res.TotalCount)
	got := res.Results[0].(*models.FileAttachment)
	require.Equal(t, "attached.txt", got.Name)
}

func TestListFileAttachments_PersonalityIDFilter_UnionWithExpressionImages(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	personalityID := createFATestPersonality(t, ds, userID, "Vix")
	otherPersonalityID := createFATestPersonality(t, ds, userID, "Nix")

	// Direct doc attachment with personality FK.
	doc, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "doc.txt",
		FileType:      "text/plain",
		PersonalityID: &personalityID,
	})
	require.NoError(t, err)

	// Expression image: file attachment with no direct personality FK, linked via
	// PersonalityExpression.
	exprImage, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "expression-happy.png",
		FileType: "image/png",
	})
	require.NoError(t, err)
	_, err = ds.dbClient.PersonalityExpression.Create().
		SetPersonalityID(personalityID).
		SetImageID(exprImage.ID).
		SetExpressionKey("happy").
		SetCreatedAt(time.Now().UTC()).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	require.NoError(t, err)

	// Attachment belonging to a different personality; must not leak in.
	_, err = ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "other-personality-doc.txt",
		FileType:      "text/plain",
		PersonalityID: &otherPersonalityID,
	})
	require.NoError(t, err)

	res, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{PersonalityID: &personalityID})
	require.NoError(t, err)
	require.Equal(t, 2, res.TotalCount)
	names := []string{}
	for _, r := range res.Results {
		names = append(names, r.(*models.FileAttachment).Name)
	}
	require.ElementsMatch(t, []string{doc.Name, exprImage.Name}, names)
}

func TestListFileAttachments_PersonalityIDFilter_DocsOnlyExcludesImages(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	personalityID := createFATestPersonality(t, ds, userID, "Vix")

	doc, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "doc.txt",
		FileType:      "text/plain",
		PersonalityID: &personalityID,
	})
	require.NoError(t, err)

	// Image with a direct personality FK (e.g. a cover photo) must be excluded when
	// DocsOnly is set, since docs can never be images per the upload form.
	_, err = ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "cover.png",
		FileType:      "image/png",
		PersonalityID: &personalityID,
	})
	require.NoError(t, err)

	docsOnly := true
	res, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{
		PersonalityID: &personalityID,
		DocsOnly:      &docsOnly,
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.TotalCount)
	got := res.Results[0].(*models.FileAttachment)
	require.Equal(t, doc.Name, got.Name)
}

func TestListFileAttachments_GlobalOnlyFilter(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	personalityID := createFATestPersonality(t, ds, userID, "Vix")

	global, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "global.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	_, err = ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "personality-doc.txt",
		FileType:      "text/plain",
		PersonalityID: &personalityID,
	})
	require.NoError(t, err)

	exprImage, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "expression-happy.png",
		FileType: "image/png",
	})
	require.NoError(t, err)
	_, err = ds.dbClient.PersonalityExpression.Create().
		SetPersonalityID(personalityID).
		SetImageID(exprImage.ID).
		SetExpressionKey("happy").
		SetCreatedAt(time.Now().UTC()).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	require.NoError(t, err)

	globalOnly := true
	res, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{GlobalOnly: &globalOnly})
	require.NoError(t, err)
	require.Equal(t, 1, res.TotalCount)
	got := res.Results[0].(*models.FileAttachment)
	require.Equal(t, global.Name, got.Name)
}

func TestListFileAttachments_DateRangeFilter(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	old, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "old.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)
	// CreatedAt isn't mutable via the ent update builder, so backdate it with raw SQL.
	_, err = ds.sqlDB.Exec(`UPDATE file_attachments SET created_at = ? WHERE id = ?`,
		time.Now().UTC().AddDate(0, 0, -30), old.ID.String())
	require.NoError(t, err)

	recent, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "recent.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	minDate := time.Now().UTC().AddDate(0, 0, -1)
	res, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{MinDate: &minDate})
	require.NoError(t, err)
	require.Equal(t, 1, res.TotalCount)
	got := res.Results[0].(*models.FileAttachment)
	require.Equal(t, recent.Name, got.Name)

	maxDate := time.Now().UTC().AddDate(0, 0, -10)
	res, err = ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{MaxDate: &maxDate})
	require.NoError(t, err)
	require.Equal(t, 1, res.TotalCount)
	got = res.Results[0].(*models.FileAttachment)
	require.Equal(t, old.Name, got.Name)
}

func TestUpdateFileAttachment_HappyPath(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, userID, modelID)
	chatMessageID := createFATestChatMessage(t, ds, chatID)

	created, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	updated, err := ds.UpdateFileAttachment(ctx, userID, created.ID, models.FileAttachment{
		ChatMessageID: &chatMessageID,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.ChatMessageID)
	require.Equal(t, chatMessageID, *updated.ChatMessageID)
}

func TestUpdateFileAttachment_NotFound(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, userID, modelID)
	chatMessageID := createFATestChatMessage(t, ds, chatID)

	_, err := ds.UpdateFileAttachment(ctx, userID, uuid.New(), models.FileAttachment{
		ChatMessageID: &chatMessageID,
	})
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestUpdateFileAttachment_WrongOwner(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createFATestUser(t, ds)
	otherUserID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, ownerID, modelID)
	chatMessageID := createFATestChatMessage(t, ds, chatID)

	created, err := ds.CreateFileAttachment(ctx, ownerID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	_, err = ds.UpdateFileAttachment(ctx, otherUserID, created.ID, models.FileAttachment{
		ChatMessageID: &chatMessageID,
	})
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestUpdateFileAttachment_InvalidRequestBody(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	created, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	// Neither FileID nor ChatMessageID set is rejected as an invalid update.
	_, err = ds.UpdateFileAttachment(ctx, userID, created.ID, models.FileAttachment{})
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestDeleteFileAttachment_HappyPath(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	created, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	err = ds.DeleteFileAttachment(ctx, userID, created.ID)
	require.NoError(t, err)

	_, err = ds.GetFileAttachment(ctx, userID, created.ID)
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestDeleteFileAttachment_NotFound(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()

	userID := createFATestUser(t, ds)
	err := ds.DeleteFileAttachment(context.Background(), userID, uuid.New())
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestDeleteFileAttachment_WrongOwner(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createFATestUser(t, ds)
	otherUserID := createFATestUser(t, ds)

	created, err := ds.CreateFileAttachment(ctx, ownerID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	err = ds.DeleteFileAttachment(ctx, otherUserID, created.ID)
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestSetFileAttachmentS3Key_HappyPath(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	created, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	err = ds.SetFileAttachmentS3Key(ctx, userID, created.ID, "users/u/file.txt")
	require.NoError(t, err)

	got, err := ds.GetFileAttachment(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, "users/u/file.txt", got.S3Key)
}

func TestSetFileAttachmentS3Key_NotFound(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()

	userID := createFATestUser(t, ds)
	err := ds.SetFileAttachmentS3Key(context.Background(), userID, uuid.New(), "key")
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestSetFileAttachmentS3Key_WrongOwner(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createFATestUser(t, ds)
	otherUserID := createFATestUser(t, ds)

	created, err := ds.CreateFileAttachment(ctx, ownerID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	err = ds.SetFileAttachmentS3Key(ctx, otherUserID, created.ID, "key")
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestUpdateFileAttachmentName_HappyPath(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	created, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "old-name.txt",
		FileType: "text/plain",
		S3Key:    "users/u/old-name.txt",
	})
	require.NoError(t, err)

	updated, err := ds.UpdateFileAttachmentName(ctx, userID, created.ID, "new-name.txt")
	require.NoError(t, err)
	require.Equal(t, "new-name.txt", updated.Name)
	// The S3 key is left unchanged so existing objects remain retrievable.
	require.Equal(t, "users/u/old-name.txt", updated.S3Key)
}

func TestUpdateFileAttachmentName_NotFound(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()

	userID := createFATestUser(t, ds)
	_, err := ds.UpdateFileAttachmentName(context.Background(), userID, uuid.New(), "new-name.txt")
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestUpdateFileAttachmentName_WrongOwner(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createFATestUser(t, ds)
	otherUserID := createFATestUser(t, ds)

	created, err := ds.CreateFileAttachment(ctx, ownerID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	_, err = ds.UpdateFileAttachmentName(ctx, otherUserID, created.ID, "new-name.txt")
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestUpdateFileAttachmentName_EmptyName(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	created, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	// UpdateFileAttachmentName itself does not validate the name, but the ent schema's
	// "name" field has a minimum-length validator, so an empty name still surfaces as a
	// generic ent validation error rather than the datastore's own ErrInvalidRequestBody.
	_, err = ds.UpdateFileAttachmentName(ctx, userID, created.ID, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "less than the required length")
}

func TestCreateFileAttachmentReference_HappyPath(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	src, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "gallery-image.png",
		FileType: "image/png",
		S3Key:    "users/u/images/gallery-image.png",
	})
	require.NoError(t, err)

	ref, err := ds.CreateFileAttachmentReference(ctx, userID, src.ID)
	require.NoError(t, err)
	require.NotEqual(t, src.ID, ref.ID)
	require.Equal(t, src.Name, ref.Name)
	require.Equal(t, src.FileType, ref.FileType)
	require.Equal(t, src.S3Key, ref.S3Key)
}

func TestCreateFileAttachmentReference_SynthesizesLegacyS3Key(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	// Legacy record predating the s3_key column: no S3Key and no chat association.
	src, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     "legacy-image.png",
		FileType: "image/png",
	})
	require.NoError(t, err)
	require.Equal(t, "", src.S3Key)

	ref, err := ds.CreateFileAttachmentReference(ctx, userID, src.ID)
	require.NoError(t, err)
	require.NotEmpty(t, ref.S3Key)
	require.Contains(t, ref.S3Key, "images")
}

func TestCreateFileAttachmentReference_SourceNotFound(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()

	userID := createFATestUser(t, ds)
	_, err := ds.CreateFileAttachmentReference(context.Background(), userID, uuid.New())
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestCreateFileAttachmentReference_SourceNotOwned(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createFATestUser(t, ds)
	otherUserID := createFATestUser(t, ds)

	src, err := ds.CreateFileAttachment(ctx, ownerID, models.FileAttachment{
		Name:     "file.txt",
		FileType: "text/plain",
	})
	require.NoError(t, err)

	_, err = ds.CreateFileAttachmentReference(ctx, otherUserID, src.ID)
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestChatHasSearchableFiles(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, userID, modelID)
	chatMessageID := createFATestChatMessage(t, ds, chatID)

	// No attached files at all yet.
	has, err := ds.ChatHasSearchableFiles(ctx, userID, chatID)
	require.NoError(t, err)
	require.False(t, has)

	created, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "doc.txt",
		FileType:      "text/plain",
		ChatMessageID: &chatMessageID,
	})
	require.NoError(t, err)

	// Attached but not yet chunked (chunk_status defaults to NULL, not "chunked").
	has, err = ds.ChatHasSearchableFiles(ctx, userID, chatID)
	require.NoError(t, err)
	require.False(t, has)

	_, err = ds.dbClient.FileAttachment.UpdateOneID(created.ID).
		SetChunkStatus("chunked").
		Save(ctx)
	require.NoError(t, err)

	has, err = ds.ChatHasSearchableFiles(ctx, userID, chatID)
	require.NoError(t, err)
	require.True(t, has)
}

func TestChatHasSearchableFiles_WrongOwner(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createFATestUser(t, ds)
	otherUserID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, ownerID, modelID)
	chatMessageID := createFATestChatMessage(t, ds, chatID)

	created, err := ds.CreateFileAttachment(ctx, ownerID, models.FileAttachment{
		Name:          "doc.txt",
		FileType:      "text/plain",
		ChatMessageID: &chatMessageID,
	})
	require.NoError(t, err)
	_, err = ds.dbClient.FileAttachment.UpdateOneID(created.ID).
		SetChunkStatus("chunked").
		Save(ctx)
	require.NoError(t, err)

	// The chat has a searchable file, but the query is scoped to otherUserID, who does
	// not own the attachment (HasOwnerWith(user.ID(userID)) filters it out).
	has, err := ds.ChatHasSearchableFiles(ctx, otherUserID, chatID)
	require.NoError(t, err)
	require.False(t, has)
}

func TestPersonalityHasSearchableFiles(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	personalityID := createFATestPersonality(t, ds, userID, "Vix")

	has, err := ds.PersonalityHasSearchableFiles(ctx, personalityID)
	require.NoError(t, err)
	require.False(t, has)

	created, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          "doc.txt",
		FileType:      "text/plain",
		PersonalityID: &personalityID,
	})
	require.NoError(t, err)

	has, err = ds.PersonalityHasSearchableFiles(ctx, personalityID)
	require.NoError(t, err)
	require.False(t, has)

	_, err = ds.dbClient.FileAttachment.UpdateOneID(created.ID).
		SetChunkStatus("chunked").
		Save(ctx)
	require.NoError(t, err)

	has, err = ds.PersonalityHasSearchableFiles(ctx, personalityID)
	require.NoError(t, err)
	require.True(t, has)
}

func TestPersonalityHasSearchableFiles_NoAttachments(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()

	has, err := ds.PersonalityHasSearchableFiles(context.Background(), uuid.New())
	require.NoError(t, err)
	require.False(t, has)
}
