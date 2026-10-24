package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
)

func relayTestAgent(t *testing.T) (*Agent, sqlmock.Sqlmock) {
	t.Helper()
	ds, mock, cleanup := newTestDatastore(t)
	t.Cleanup(cleanup)
	a := newTestAgent(ds)
	a.relayRoute = provider.NewCheaperInferenceRoute("http://relay.invalid", true, nil, nil)
	return a, mock
}

// expectUserLookup answers GetUserByID with a user whose opt-in is optedIn, plus
// the empty preference and role eager loads.
func expectUserLookup(mock sqlmock.Sqlmock, userID uuid.UUID, optedIn bool) {
	mock.ExpectQuery(`FROM .users.`).WillReturnRows(
		sqlmock.NewRows([]string{"id", "inference_relay_opt_in"}).AddRow(userID, optedIn))
	mock.ExpectQuery(`.*`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`.*`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
}

var routedGemini = resolvedChatModel{name: "gemini-3.5-flash", provider: "google"}

func TestRelayTurnFor_OptedInUserOnRoutedModel(t *testing.T) {
	a, mock := relayTestAgent(t)
	userID := uuid.New()
	expectUserLookup(mock, userID, true)

	require.NotNil(t, a.relayTurnFor(context.Background(), userID, routedGemini))
}

func TestRelayTurnFor_UserNotOptedIn(t *testing.T) {
	a, mock := relayTestAgent(t)
	userID := uuid.New()
	expectUserLookup(mock, userID, false)

	require.Nil(t, a.relayTurnFor(context.Background(), userID, routedGemini))
}

func TestRelayTurnFor_LookupErrorGoesDirect(t *testing.T) {
	a, mock := relayTestAgent(t)
	mock.ExpectQuery(`.*`).WillReturnError(errors.New("db down"))

	require.Nil(t, a.relayTurnFor(context.Background(), uuid.New(), routedGemini))
}

// The user is loaded only when routing is possible at all, so turns on other
// models (and deployments without a relay key) pay no extra query.
func TestRelayTurnFor_SkipsLookupWhenNotRoutable(t *testing.T) {
	cases := map[string]func(a *Agent) resolvedChatModel{
		"model outside the route table": func(*Agent) resolvedChatModel {
			return resolvedChatModel{name: "gemini-3.5", provider: "google"}
		},
		"not a Chat Completions provider": func(*Agent) resolvedChatModel {
			return resolvedChatModel{name: "gemini-3.5-flash", provider: "openai"}
		},
		"no relay configured": func(a *Agent) resolvedChatModel {
			a.relayRoute = nil
			return routedGemini
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			a, mock := relayTestAgent(t)
			mock.ExpectQuery(`.*`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			resolved := setup(a)

			require.Nil(t, a.relayTurnFor(context.Background(), uuid.New(), resolved))
			require.Error(t, mock.ExpectationsWereMet(), "no user query should have run")
		})
	}
}
