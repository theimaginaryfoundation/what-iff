package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestListFileAttachmentsInChatScope(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createFATestUser(t, ds)
	modelID := createFATestModel(t, ds)
	chatID := createFATestChat(t, ds, userID, modelID)
	otherChatID := createFATestChat(t, ds, userID, modelID)
	personalityID := createFATestPersonality(t, ds, userID, "Vix")
	otherPersonalityID := createFATestPersonality(t, ds, userID, "Other")

	create := func(name string, msgChat *uuid.UUID, pid *uuid.UUID) uuid.UUID {
		t.Helper()
		in := models.FileAttachment{Name: name, FileType: "text/plain", PersonalityID: pid}
		if msgChat != nil {
			msgID := createFATestChatMessage(t, ds, *msgChat)
			in.ChatMessageID = &msgID
		}
		fa, err := ds.CreateFileAttachment(ctx, userID, in)
		require.NoError(t, err)
		return fa.ID
	}

	inChat := create("in-chat.txt", &chatID, nil)
	personalityDoc := create("persona-doc.md", nil, &personalityID)
	create("other-chat.txt", &otherChatID, nil)
	create("other-persona.md", nil, &otherPersonalityID)
	create("loose.txt", nil, nil)

	names := func(files []*models.FileAttachment) map[uuid.UUID]string {
		out := map[uuid.UUID]string{}
		for _, f := range files {
			out[f.ID] = f.Name
		}
		return out
	}

	got, err := ds.ListFileAttachmentsInChatScope(ctx, userID, chatID, &personalityID, 0)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]string{inChat: "in-chat.txt", personalityDoc: "persona-doc.md"}, names(got))

	for _, f := range got {
		if f.ID == inChat {
			require.NotNil(t, f.ChatID, "chat id is loaded so storage fallbacks can derive the key")
			require.Equal(t, chatID, *f.ChatID)
		}
	}

	chatOnly, err := ds.ListFileAttachmentsInChatScope(ctx, userID, chatID, nil, 0)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]string{inChat: "in-chat.txt"}, names(chatOnly))

	limited, err := ds.ListFileAttachmentsInChatScope(ctx, userID, chatID, &personalityID, 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)

	strangerID := createFATestUser(t, ds)
	none, err := ds.ListFileAttachmentsInChatScope(ctx, strangerID, chatID, &personalityID, 0)
	require.NoError(t, err)
	require.Empty(t, none, "another user's chat yields nothing")
}
