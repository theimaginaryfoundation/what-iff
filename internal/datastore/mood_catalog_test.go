package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func newMoodCatalogTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createFileAttachmentTestSchema)
}

func createMoodTestUser(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.dbClient.User.Create().
		SetID(id).
		SetUsername("mood-" + id.String()[:8]).
		SetEmail("mood-" + id.String()[:8] + "@example.com").
		SetPasswordHash("hash").
		Save(context.Background())
	require.NoError(t, err)
	return id
}

func createMoodTestImage(t *testing.T, ds *Datastore, userID uuid.UUID) uuid.UUID {
	t.Helper()
	fa, err := ds.dbClient.FileAttachment.Create().
		SetName("pic.jpg").
		SetFileType("image/jpeg").
		SetOwnerID(userID).
		Save(context.Background())
	require.NoError(t, err)
	return fa.ID
}

func createMoodTestRitual(t *testing.T, ds *Datastore, userID uuid.UUID) uuid.UUID {
	t.Helper()
	r, err := ds.dbClient.Ritual.Create().
		SetOwnerID(userID).
		SetName("Morning").
		SetDescription("desc").
		SetContent("content").
		SetHotkeys("").
		Save(context.Background())
	require.NoError(t, err)
	return r.ID
}

func createMoodTestPersonality(t *testing.T, ds *Datastore, userID uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	p, err := ds.dbClient.Personality.Create().
		SetName("Test Personality").
		SetSystemPrompt("system prompt").
		SetUserID(userID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	require.NoError(t, err)
	return p.ID
}

func TestCreateMood(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	imageID := createMoodTestImage(t, ds, userID)
	ritualID := createMoodTestRitual(t, ds, userID)

	got, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{
		Name:          "Cozy",
		Description:   "warm and quiet",
		PromptSnippet: "speak softly",
		ImageIDs:      []uuid.UUID{imageID},
		RitualIDs:     []uuid.UUID{ritualID},
	})
	require.NoError(t, err)
	require.Equal(t, "Cozy", got.Name)
	require.Equal(t, []uuid.UUID{imageID}, got.ImageIDs)
	require.Equal(t, []uuid.UUID{ritualID}, got.RitualIDs)
}

func TestCreateMood_RejectsNonOwnedImage(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	otherUserID := createMoodTestUser(t, ds)
	imageID := createMoodTestImage(t, ds, otherUserID)

	_, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{
		Name:     "Cozy",
		ImageIDs: []uuid.UUID{imageID},
	})
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestCreateMood_RejectsNonOwnedRitual(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	otherUserID := createMoodTestUser(t, ds)
	ritualID := createMoodTestRitual(t, ds, otherUserID)

	_, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{
		Name:      "Cozy",
		RitualIDs: []uuid.UUID{ritualID},
	})
	require.ErrorIs(t, err, ErrRitualNotFound)
}

func TestGetMood(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	got, err := ds.GetMood(context.Background(), userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
}

func TestGetMood_NotFound(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)

	_, err := ds.GetMood(context.Background(), userID, uuid.New())
	require.ErrorIs(t, err, ErrMoodNotFound)
}

func TestGetMood_WrongOwner(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	otherUserID := createMoodTestUser(t, ds)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	_, err = ds.GetMood(context.Background(), otherUserID, created.ID)
	require.ErrorIs(t, err, ErrMoodNotFound)
}

func TestListMoods(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	_, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)
	_, err = ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Excited"})
	require.NoError(t, err)

	otherUserID := createMoodTestUser(t, ds)
	_, err = ds.CreateMood(context.Background(), otherUserID, models.CreateMoodRequest{Name: "NotMine"})
	require.NoError(t, err)

	got, err := ds.ListMoods(context.Background(), userID, 1, 20, models.MoodFilters{})
	require.NoError(t, err)
	require.Equal(t, 2, got.TotalCount)
	require.Len(t, got.Results, 2)
}

func TestListMoods_NameFilter(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	_, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)
	_, err = ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Excited"})
	require.NoError(t, err)

	name := "coz"
	got, err := ds.ListMoods(context.Background(), userID, 1, 20, models.MoodFilters{Name: &name})
	require.NoError(t, err)
	require.Equal(t, 1, got.TotalCount)
}

func TestListMoods_DefaultsPageNumAndSize(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	_, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	got, err := ds.ListMoods(context.Background(), userID, 0, 0, models.MoodFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, got.Page)
	require.Equal(t, 1, got.TotalCount)
}

func TestUpdateMood(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	imageID := createMoodTestImage(t, ds, userID)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	newImageIDs := []uuid.UUID{imageID}
	got, err := ds.UpdateMood(context.Background(), userID, created.ID, models.UpdateMoodRequest{
		Name:             "Cozier",
		Description:      "even warmer",
		PromptSnippet:    "whisper",
		RecommendedModel: "gpt-5.1",
		ImageIDs:         &newImageIDs,
	})
	require.NoError(t, err)
	require.Equal(t, "Cozier", got.Name)
	require.Equal(t, "gpt-5.1", got.RecommendedModel)
	require.Equal(t, []uuid.UUID{imageID}, got.ImageIDs)
}

func TestUpdateMood_NotFound(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)

	_, err := ds.UpdateMood(context.Background(), userID, uuid.New(), models.UpdateMoodRequest{Name: "X"})
	require.ErrorIs(t, err, ErrMoodNotFound)
}

func TestUpdateMood_RejectsNonOwnedImage(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	otherUserID := createMoodTestUser(t, ds)
	imageID := createMoodTestImage(t, ds, otherUserID)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	badImageIDs := []uuid.UUID{imageID}
	_, err = ds.UpdateMood(context.Background(), userID, created.ID, models.UpdateMoodRequest{
		Name:     "Cozy",
		ImageIDs: &badImageIDs,
	})
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)
}

func TestUpdateMood_RejectsNonOwnedRitual(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	otherUserID := createMoodTestUser(t, ds)
	ritualID := createMoodTestRitual(t, ds, otherUserID)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	badRitualIDs := []uuid.UUID{ritualID}
	_, err = ds.UpdateMood(context.Background(), userID, created.ID, models.UpdateMoodRequest{
		Name:      "Cozy",
		RitualIDs: &badRitualIDs,
	})
	require.ErrorIs(t, err, ErrRitualNotFound)
}

func TestSetMoodThumbnail(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	err = ds.SetMoodThumbnail(context.Background(), created.ID, []byte("jpegdata"))
	require.NoError(t, err)

	got, err := ds.GetMood(context.Background(), userID, created.ID)
	require.NoError(t, err)
	require.NotEmpty(t, got.ThumbnailData)
}

func TestDeleteMood(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	err = ds.DeleteMood(context.Background(), userID, created.ID)
	require.NoError(t, err)

	_, err = ds.GetMood(context.Background(), userID, created.ID)
	require.ErrorIs(t, err, ErrMoodNotFound)
}

func TestDeleteMood_NotFound(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)

	err := ds.DeleteMood(context.Background(), userID, uuid.New())
	require.ErrorIs(t, err, ErrMoodNotFound)
}

func TestDeleteMood_WrongOwner(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	otherUserID := createMoodTestUser(t, ds)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	err = ds.DeleteMood(context.Background(), otherUserID, created.ID)
	require.ErrorIs(t, err, ErrMoodNotFound)
}

func TestSetMoodPersonalities(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	personalityID := createMoodTestPersonality(t, ds, userID)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	err = ds.SetMoodPersonalities(context.Background(), userID, created.ID, []uuid.UUID{personalityID})
	require.NoError(t, err)

	got, err := ds.GetMood(context.Background(), userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{personalityID}, got.PersonalityIDs)

	// Clearing with an empty slice removes the association.
	err = ds.SetMoodPersonalities(context.Background(), userID, created.ID, nil)
	require.NoError(t, err)
	got, err = ds.GetMood(context.Background(), userID, created.ID)
	require.NoError(t, err)
	require.Empty(t, got.PersonalityIDs)
}

func TestSetMoodPersonalities_MoodNotFound(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)

	err := ds.SetMoodPersonalities(context.Background(), userID, uuid.New(), nil)
	require.ErrorIs(t, err, ErrMoodNotFound)
}

func TestSetMoodPersonalities_RejectsNonOwnedPersonality(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	otherUserID := createMoodTestUser(t, ds)
	personalityID := createMoodTestPersonality(t, ds, otherUserID)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	err = ds.SetMoodPersonalities(context.Background(), userID, created.ID, []uuid.UUID{personalityID})
	require.ErrorIs(t, err, ErrPersonalityNotFound)
}

func TestGetMoodsForPersonality(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	personalityID := createMoodTestPersonality(t, ds, userID)
	created, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)
	require.NoError(t, ds.SetMoodPersonalities(context.Background(), userID, created.ID, []uuid.UUID{personalityID}))

	got, err := ds.GetMoodsForPersonality(context.Background(), userID, personalityID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, created.ID, got[0].ID)
}

func TestGetMoodsForPersonality_NoneAttached(t *testing.T) {
	ds, cleanup := newMoodCatalogTestDatastore(t)
	defer cleanup()

	userID := createMoodTestUser(t, ds)
	personalityID := createMoodTestPersonality(t, ds, userID)
	_, err := ds.CreateMood(context.Background(), userID, models.CreateMoodRequest{Name: "Cozy"})
	require.NoError(t, err)

	got, err := ds.GetMoodsForPersonality(context.Background(), userID, personalityID)
	require.NoError(t, err)
	require.Empty(t, got)
}
