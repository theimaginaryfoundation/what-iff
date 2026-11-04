package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/discordrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type statusCall struct {
	status models.DiscordBotStatus
	reason string
}

type fakeStatusStore struct {
	mu    sync.Mutex
	calls map[uuid.UUID][]statusCall
}

func (f *fakeStatusStore) SetDiscordBotStatus(_ context.Context, id uuid.UUID, status models.DiscordBotStatus, lastError string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[uuid.UUID][]statusCall{}
	}
	f.calls[id] = append(f.calls[id], statusCall{status, lastError})
	return nil
}

func (f *fakeStatusStore) callsFor(id uuid.UUID) []statusCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

// The error discordgo's Open returns for a Gateway close, as DiscordgoDialer wraps it.
func gatewayClose(code int, text string) error {
	return classifyClose(fmt.Errorf("discord gateway open: %w", &websocket.CloseError{Code: code, Text: text}))
}

func TestClassifyCloseMarksCodesRetryingCannotFix(t *testing.T) {
	assert.ErrorIs(t, gatewayClose(4004, "Authentication failed."), discordrelay.ErrInvalidToken)
	assert.ErrorIs(t, gatewayClose(4014, "Disallowed intent(s)."), discordrelay.ErrDisallowedIntents)

	other := gatewayClose(4000, "Unknown error.")
	assert.NotErrorIs(t, other, discordrelay.ErrInvalidToken)
	assert.NotErrorIs(t, other, discordrelay.ErrDisallowedIntents)
	assert.Contains(t, gatewayClose(4004, "Authentication failed.").Error(), "close 4004", "the original close stays in the message")
}

func TestReconcileRecordsBotsDiscordRefuses(t *testing.T) {
	revoked := Bot{ID: uuid.New(), Token: "t1"}
	intentOff := Bot{ID: uuid.New(), Token: "t2", MessageContent: true}
	flaky := Bot{ID: uuid.New(), Token: "t3"}
	refusals := map[uuid.UUID]error{
		revoked.ID:   gatewayClose(4004, "Authentication failed."),
		intentOff.ID: gatewayClose(4014, "Disallowed intent(s)."),
		flaky.ID:     errors.New("dial tcp: i/o timeout"),
	}
	store := &fakeStatusStore{}
	s := &Supervisor{
		Source: &fakeSource{bots: []Bot{revoked, intentOff, flaky}},
		Sink:   &recordingSink{},
		Dial: func(_ context.Context, bot Bot, _ Handlers) (Conn, error) {
			return nil, refusals[bot.ID]
		},
		Status: store,
		Logger: zap.NewNop(),
	}

	s.Reconcile(context.Background())

	require.Equal(t, []statusCall{{models.DiscordBotInvalidToken, discordrelay.InvalidTokenReason}}, store.callsFor(revoked.ID))
	require.Equal(t, []statusCall{{models.DiscordBotIntentDisabled, discordrelay.DisallowedIntentsReason}}, store.callsFor(intentOff.ID))
	assert.Empty(t, store.callsFor(flaky.ID), "a transient failure leaves the bot active for the next reconcile")
	assert.Empty(t, s.Connected())
}
