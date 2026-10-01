// Package plugins is the extension seam for optional features that are not part
// of this repository. It is a supported API: an operator who wants routes this
// project does not ship writes a package whose init() calls Register, links it
// with a blank import in cmd/api-server, and Apply wires it into the router
// during server setup.
//
// The core registers nothing itself. With no plugin linked — the default —
// Apply iterates an empty list and the server behaves exactly as it would
// without this package. Adding a plugin therefore requires no change to any
// file here, which is the point: the seam absorbs the divergence so extensions
// do not have to patch the tree they extend.
//
// Deps is what a plugin may build on. It grows deliberately; see the notes on
// its fields for the two rules that keep it a capability set rather than a
// grab-bag of server internals.
package plugins

import (
	"context"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Deps is the wiring a plugin receives at route-registration time — the subset
// of server internals a plugin may build on. Extend it as new plugins need more.
type Deps struct {
	DataStore *datastore.Datastore
	Logger    *zap.Logger
	// PublicRouter serves unauthenticated /api routes.
	PublicRouter *mux.Router
	// AuthRouter serves authenticated /api routes (behind AuthMiddleware).
	AuthRouter *mux.Router
	// V1AuthRouter serves authenticated /api/v1 routes (behind AuthMiddleware).
	// A plugin mounting here owns its own authorization: the router carries
	// authentication only, so a staff-only surface must apply its own
	// middleware.RequireRole subrouter, exactly as the public role handler does.
	V1AuthRouter *mux.Router
	// FileStore is the configured object store, for plugins that move user
	// artifacts (archive import/export and the like).
	FileStore storage.FileStore
	// CreateEmbedding generates an embedding vector, or is nil when no
	// embedding provider is configured — callers must check before use.
	//
	// It is a function rather than an API key plus an *http.Client so that the
	// choice of transport stays here: under a non-vendor LLM_BACKEND the server
	// builds this over the deny-network client, and a plugin cannot opt out of
	// that by constructing its own (ADR 0x018).
	CreateEmbedding func(ctx context.Context, input string) ([]float32, error)
	// Lifecycle is cancelled when the server begins shutting down. A plugin that
	// runs background work (a poller, a long-lived connection) runs it on this
	// context, so it stops with the server instead of being cut off mid-write.
	Lifecycle context.Context
	// Turns starts agent turns on a user's behalf, for plugins that receive
	// messages from outside the app (another chat service, say). It is nil when
	// the server was built without an agent; callers must check before use.
	Turns TurnStarter
}

// UserTurn is one message to answer as if the user had sent it in the app.
type UserTurn struct {
	// UserID owns the chat. The turn runs, and is metered, as this user.
	UserID uuid.UUID
	// Timezone is the user's IANA timezone, used for timestamps the agent sees.
	// Empty means the server default.
	Timezone string
	// Message is saved as a user message. ChatID and Message are required; Origin
	// is always set to user.
	Message models.ChatMessage
}

// TurnStarter starts an agent turn the same way the app and the webhook user mode
// do: the message is saved, a tracking job is created, and the reply is written
// asynchronously. The returned response carries the saved message id and the job
// id. The reply's completion is observable through internal/replyhook.
//
// Ownership is enforced by the datastore, as it is for every turn: a chat that
// does not belong to UserID is not found.
type TurnStarter interface {
	StartUserTurn(ctx context.Context, turn UserTurn) (*models.ChatMessageResponse, error)
}

// Registrar wires one plugin's routes/handlers onto the server.
type Registrar func(Deps)

var registrars []Registrar

// Register adds a plugin registrar. Call it from an init() in the plugin
// package; linking that package (a blank import in cmd/api-server) is what
// activates the plugin. Not safe for concurrent use — registration happens
// during package init, before Apply.
func Register(r Registrar) { registrars = append(registrars, r) }

// Apply runs every registered plugin against d. It is a no-op when no plugin is
// linked, which is the default.
func Apply(d Deps) {
	for _, r := range registrars {
		r(d)
	}
}
