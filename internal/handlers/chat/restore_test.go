package chat

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestRestoreChat_ValidatesBeforeMutation(t *testing.T) {
	for _, body := range []string{`{"unknown":true}`, `{"guidance":5}`, `{} {}`, `{"guidance":"` + strings.Repeat("x", 4001) + `"}`, `{`} {
		t.Run(body[:min(25, len(body))], func(t *testing.T) {
			router, _ := newRehydrationHandler(t, &fakeStore{})
			w := serve(router, uuid.New(), http.MethodPost, "/chat/"+uuid.NewString()+"/restore", body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}

func TestRestoreChat_ExplicitGuidanceTriggersPreparation(t *testing.T) {
	for _, guidance := range []string{"", "Keep decisions. Omit old drafts."} {
		t.Run(guidance, func(t *testing.T) {
			owner, id := uuid.New(), uuid.New()
			called := 0
			router, mock := newRehydrationHandler(t, &fakeStore{
				getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
					return &models.Chat{ID: id, Archived: boolPtr(true)}, nil
				},
				restoreChatFn: func(_ context.Context, gotOwner, gotID uuid.UUID, gotGuidance string) (*models.Chat, error) {
					called++
					require.Equal(t, owner, gotOwner)
					require.Equal(t, id, gotID)
					require.Equal(t, guidance, gotGuidance)
					return &models.Chat{ID: id, Archived: boolPtr(false), RestoreGuidance: gotGuidance, RehydrationState: models.RehydrationStateStale}, nil
				},
			})
			mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 0))
			w := serve(router, owner, http.MethodPost, "/chat/"+id.String()+"/restore", `{"guidance":" `+guidance+` "}`)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, called)
			if guidance != "" {
				require.NoError(t, mock.ExpectationsWereMet())
			} else {
				require.Error(t, mock.ExpectationsWereMet(), "blank restore must not start costly work")
			}
		})
	}
}

func TestRestoreChat_NotFoundAndConflict(t *testing.T) {
	for _, missing := range []bool{true, false} {
		owner, id := uuid.New(), uuid.New()
		router, _ := newRehydrationHandler(t, &fakeStore{
			getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
				if missing {
					return nil, datastore.ErrChatNotFound
				}
				return &models.Chat{ID: id}, nil
			},
			restoreChatFn: func(context.Context, uuid.UUID, uuid.UUID, string) (*models.Chat, error) {
				return nil, datastore.ErrChatRestoreConflict
			},
		})
		w := serve(router, owner, http.MethodPost, "/chat/"+id.String()+"/restore", `{}`)
		if missing {
			require.Equal(t, http.StatusNotFound, w.Code)
		} else {
			require.Equal(t, http.StatusConflict, w.Code)
		}
	}
}
