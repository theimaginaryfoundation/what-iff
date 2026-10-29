package providerconnections

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/reauth"
)

// CustomAddressesAllowed reports whether accounts may choose a provider's
// address: Custom, and the presets for servers on this computer. It is a fact
// the deployment states on GET /provider-access. A deployment that serves the
// public replaces it in an init() to answer false, since reaching an address an
// account typed needs a guard against server-side request forgery that this
// package does not have (ADR 0x027).
var CustomAddressesAllowed = func() bool { return true }

const maxNameLen = 100

// Handler serves the routes an account manages its connections through.
type Handler struct {
	store   *Store
	lister  *Lister
	presets []Preset
	confirm reauth.Confirmer
	logger  *zap.Logger
}

// NewHandler builds the routes over the store, a Lister and the presets the
// deployment offers. confirm checks that whoever adds or redirects a connection
// is the account's owner, not just someone holding its session.
func NewHandler(store *Store, lister *Lister, presets []Preset, confirm reauth.Confirmer, logger *zap.Logger) *Handler {
	return &Handler{store: store, lister: lister, presets: presets, confirm: confirm, logger: logger}
}

// RegisterRoutes mounts the routes on an authenticated router.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/provider-connections", h.handleList).Methods(http.MethodGet)
	router.HandleFunc("/provider-connections", h.handleCreate).Methods(http.MethodPost)
	router.HandleFunc("/provider-connections/presets", h.handlePresets).Methods(http.MethodGet)
	router.HandleFunc("/provider-connections/{id}", h.handleUpdate).Methods(http.MethodPatch)
	router.HandleFunc("/provider-connections/{id}", h.handleDelete).Methods(http.MethodDelete)
	router.HandleFunc("/provider-connections/{id}/models", h.handleModels).Methods(http.MethodGet)
	router.HandleFunc("/provider-connections/{id}/models", h.handleSetModels).Methods(http.MethodPut)
}

// offered lists the presets this deployment lets accounts use.
func (h *Handler) offered() []Preset {
	allowed := CustomAddressesAllowed()
	out := make([]Preset, 0, len(h.presets))
	for _, p := range h.presets {
		if p.AddressEditable && !allowed {
			continue
		}
		out = append(out, p)
	}
	return out
}

// --- Helpers

func (h *Handler) requireAccount(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
	}
	return userID, ok
}

// confirmOwner answers the request itself and returns false when the owner did
// not confirm.
func (h *Handler) confirmOwner(w http.ResponseWriter, r *http.Request, userID uuid.UUID, password string) bool {
	err := h.confirm.Confirm(r.Context(), userID, password)
	if err == nil {
		return true
	}
	if errors.Is(err, reauth.ErrNotConfirmed) {
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, models.ErrCodeReauthenticationRequired,
			"Confirm your current password to change a provider connection", nil)
		return false
	}
	handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to confirm the change", err)
	return false
}

// addressAllowed answers the request itself and returns false when the
// connection's address is the account's choice and this deployment does not
// allow that. It guards every route that reaches such an address, not only
// adding one, so turning the setting off also stops connections already stored.
func (h *Handler) addressAllowed(w http.ResponseWriter, preset Preset) bool {
	if !preset.AddressEditable || CustomAddressesAllowed() {
		return true
	}
	handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, models.ErrCodeCustomAddressNotAllowed,
		"This server does not allow choosing a provider's address", nil)
	return false
}

// respondConnectError answers a failed listing with its stable code.
func (h *Handler) respondConnectError(w http.ResponseWriter, preset string, err error) {
	var ce *ConnectError
	if !errors.As(err, &ce) {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list the provider's models", err)
		return
	}
	messages := map[string]string{
		models.ErrCodeProviderUnreachable:  "Nothing answered at the provider's address",
		models.ErrCodeProviderKeyRejected:  "The provider rejected the key",
		models.ErrCodeProviderIncompatible: "The address did not answer as that kind of server",
		models.ErrCodeProviderUnavailable:  "The provider is not answering right now",
	}
	// The cause is logged, never returned: it can carry the provider's own text.
	h.logger.Info("provider listing failed", zap.String("preset", preset), zap.String("code", ce.Code), zap.Error(ce.Err))
	handlerutils.RespondWithErrorDetails(w, h.logger, http.StatusUnprocessableEntity, ce.Code, messages[ce.Code],
		map[string]string{"preset": preset}, nil)
}

// respondExists answers an attempt to add a second connection to a built-in
// provider.
func (h *Handler) respondExists(w http.ResponseWriter) {
	handlerutils.RespondWithError(w, h.logger, http.StatusConflict, models.ErrCodeProviderConnectionExists, ErrBuiltinExists.Error(), nil)
}

func badRequest(w http.ResponseWriter, logger *zap.Logger, message string) {
	handlerutils.RespondWithError(w, logger, http.StatusBadRequest, handlerutils.CodeNotSet, message, nil)
}

// --- Routes

func (h *Handler) handlePresets(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAccount(w, r); !ok {
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, h.offered())
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	conns, err := h.store.List(r.Context(), userID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list provider connections", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, conns)
}

type createRequest struct {
	Preset  string `json:"preset"`
	Name    string `json:"name"`
	Flavor  Flavor `json:"flavor"`
	BaseURL string `json:"base_url"`
	Key     string `json:"key"`
	// CurrentPassword confirms the change. A session alone is not enough to
	// redirect where the account's conversations are sent.
	CurrentPassword string `json:"current_password"`
}

// connected is what adding a connection answers with: the connection, and the
// models its provider offers.
type connected struct {
	Connection Connection    `json:"connection"`
	Models     []ListedModel `json:"models"`
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := handlerutils.DecodeJSON(r, &req); err != nil {
		badRequest(w, h.logger, "Invalid request body")
		return
	}
	preset, ok := findPreset(h.presets, req.Preset)
	if !ok {
		badRequest(w, h.logger, "Unknown preset")
		return
	}
	if !h.addressAllowed(w, preset) {
		return
	}
	nc, flavor, listURL, msg := resolveNew(preset, req)
	if msg != "" {
		badRequest(w, h.logger, msg)
		return
	}
	if !h.confirmOwner(w, r, userID, req.CurrentPassword) {
		return
	}
	if preset.Builtin {
		exists, err := h.store.hasBuiltin(r.Context(), userID, preset.ID)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to add the provider connection", err)
			return
		}
		if exists {
			h.respondExists(w)
			return
		}
	}

	listed, err := h.lister.List(r.Context(), preset.target(flavor, listURL, nc.Key))
	if err != nil {
		h.respondConnectError(w, preset.ID, err)
		return
	}
	conn, err := h.store.Create(r.Context(), userID, nc)
	switch {
	case errors.Is(err, ErrBuiltinExists):
		h.respondExists(w)
	case err != nil:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to add the provider connection", err)
	default:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, connected{Connection: conn, Models: listed})
	}
}

// resolveNew checks a new connection against its preset. It returns the
// connection to store, the flavor and address to list it at, and a message
// when the request is not valid. Only Custom stores its flavor; a preset
// implies its own.
func resolveNew(preset Preset, req createRequest) (nc NewConnection, flavor Flavor, listURL, msg string) {
	nc = NewConnection{
		Preset:  preset.ID,
		Builtin: preset.Builtin,
		Name:    strings.TrimSpace(req.Name),
		Key:     strings.TrimSpace(req.Key),
	}
	flavor = preset.Flavor
	if nc.Name == "" {
		nc.Name = preset.Name
	}
	if len(nc.Name) > maxNameLen {
		return nc, "", "", "Name must be at most 100 characters"
	}
	if preset.ID == PresetCustom {
		if !req.Flavor.Valid() {
			return nc, "", "", "Choose what the server speaks: openai or anthropic"
		}
		flavor, nc.Flavor = req.Flavor, req.Flavor
		if strings.TrimSpace(req.Name) == "" {
			return nc, "", "", "Name the connection"
		}
	}
	if nc.Key == "" && preset.KeyRequired {
		return nc, "", "", "This provider needs an API key"
	}
	if nc.Key != "" {
		if _, err := checkKey(nc.Key); err != nil {
			return nc, "", "", err.Error()
		}
	}

	listURL = preset.BaseURL
	if preset.AddressEditable {
		raw := req.BaseURL
		if strings.TrimSpace(raw) == "" {
			raw = preset.BaseURL
		}
		if strings.TrimSpace(raw) == "" {
			return nc, "", "", "Enter the server's address"
		}
		addr, err := normalizeAddress(raw)
		if err != nil {
			return nc, "", "", err.Error()
		}
		listURL = addr
		nc.BaseURL = storedAddress(addr, preset)
	} else if strings.TrimSpace(req.BaseURL) != "" {
		return nc, "", "", "This provider's address is set by its preset"
	}
	return nc, flavor, listURL, ""
}

type updateRequest struct {
	Name            *string `json:"name"`
	BaseURL         *string `json:"base_url"`
	Key             *string `json:"key"`
	CurrentPassword string  `json:"current_password"`
}

func (h *Handler) connectionFor(w http.ResponseWriter, r *http.Request, userID uuid.UUID) (*ent.ProviderConnection, Preset, bool) {
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "No such provider connection", nil)
		return nil, Preset{}, false
	}
	row, err := h.store.get(r.Context(), userID, id)
	if errors.Is(err, ErrNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "No such provider connection", nil)
		return nil, Preset{}, false
	}
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the provider connection", err)
		return nil, Preset{}, false
	}
	return row, presetOf(h.presets, row.Preset), true
}

// storedAddress is what a connection stores for an address the account gave:
// empty when it is the preset's own, so the connection follows the preset if
// its default changes (as a local server's does with LOCAL_SERVER_HOST), and
// the address otherwise.
func storedAddress(addr string, preset Preset) string {
	if preset.BaseURL != "" {
		if def, err := normalizeAddress(preset.BaseURL); err == nil && def == addr {
			return ""
		}
	}
	return addr
}

// flavorOf is the API a stored connection speaks.
func flavorOf(row *ent.ProviderConnection, preset Preset) Flavor {
	if row.Flavor != "" {
		return Flavor(row.Flavor)
	}
	return preset.Flavor
}

// addressOf is where a stored connection is reached.
func addressOf(row *ent.ProviderConnection, preset Preset) string {
	if row.BaseURL != "" {
		return row.BaseURL
	}
	return preset.BaseURL
}

func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	row, preset, ok := h.connectionFor(w, r, userID)
	if !ok {
		return
	}
	var req updateRequest
	if err := handlerutils.DecodeJSON(r, &req); err != nil {
		badRequest(w, h.logger, "Invalid request body")
		return
	}

	changes := Changes{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > maxNameLen {
			badRequest(w, h.logger, "Name must be 1 to 100 characters")
			return
		}
		changes.Name = &name
	}
	address := addressOf(row, preset)
	if req.BaseURL != nil {
		if !preset.AddressEditable {
			badRequest(w, h.logger, "This provider's address is set by its preset")
			return
		}
		addr, err := normalizeAddress(*req.BaseURL)
		if err != nil {
			badRequest(w, h.logger, err.Error())
			return
		}
		stored := storedAddress(addr, preset)
		changes.BaseURL, address = &stored, addr
	}
	if req.Key != nil {
		key := strings.TrimSpace(*req.Key)
		if key == "" && preset.KeyRequired {
			badRequest(w, h.logger, "This provider needs an API key")
			return
		}
		if key != "" {
			if _, err := checkKey(key); err != nil {
				badRequest(w, h.logger, err.Error())
				return
			}
		}
		changes.Key = &key
	}

	// Pointing a connection somewhere else, or at someone else's key, needs the
	// owner, and is checked before it is saved. A rename needs neither.
	var listed []ListedModel
	if changes.BaseURL != nil || changes.Key != nil {
		if !h.addressAllowed(w, preset) || !h.confirmOwner(w, r, userID, req.CurrentPassword) {
			return
		}
		key, err := h.keyFor(row, changes)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the provider connection", err)
			return
		}
		listed, err = h.lister.List(r.Context(), preset.target(flavorOf(row, preset), address, key))
		if err != nil {
			h.respondConnectError(w, preset.ID, err)
			return
		}
	}

	conn, err := h.store.Update(r.Context(), userID, row.ID, changes)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update the provider connection", err)
		return
	}
	if listed == nil {
		listed = []ListedModel{}
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, connected{Connection: conn, Models: listed})
}

// keyFor is the key a connection will have after changes.
func (h *Handler) keyFor(row *ent.ProviderConnection, changes Changes) (string, error) {
	if changes.Key != nil {
		return *changes.Key, nil
	}
	return h.store.keyOf(row)
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	row, _, ok := h.connectionFor(w, r, userID)
	if !ok {
		return
	}
	inUse, err := h.store.defaultModelOn(r.Context(), userID, row.ID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete the provider connection", err)
		return
	}
	if inUse {
		h.respondDefaultInUse(w)
		return
	}
	// Its models go with it (ADR 0x027): a chat on one falls back to the
	// default model, and a job on one fails clearly.
	if err := h.store.Delete(r.Context(), userID, row.ID); err != nil && !errors.Is(err, ErrNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete the provider connection", err)
		return
	}
	handlerutils.RespondWithNoContent(w)
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	row, preset, ok := h.connectionFor(w, r, userID)
	if !ok || !h.addressAllowed(w, preset) {
		return
	}
	key, err := h.store.keyOf(row)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the provider connection", err)
		return
	}
	flavor := flavorOf(row, preset)
	listed, err := h.lister.List(r.Context(), preset.target(flavor, addressOf(row, preset), key))
	if err != nil {
		h.respondConnectError(w, preset.ID, err)
		return
	}
	merged, err := h.store.withEnabled(r.Context(), row.ID, withKnownCapabilities(preset, flavor, listed))
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the connection's models", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, merged)
}

// withKnownCapabilities fills in what is known about a flavor where the
// provider reported nothing: Anthropic-compatible servers take tools, and
// Anthropic's own models take images too. Anything still unknown is left for
// the account to confirm.
func withKnownCapabilities(preset Preset, flavor Flavor, listed []ListedModel) []ListedModel {
	yes := true
	for i := range listed {
		if flavor == FlavorAnthropic && listed[i].ToolSupport == nil {
			listed[i].ToolSupport = &yes
		}
		if preset.ID == "anthropic" && listed[i].VisionSupport == nil {
			listed[i].VisionSupport = &yes
		}
	}
	return listed
}

type setModelsRequest struct {
	Models []ModelChoice `json:"models"`
}

// handleSetModels enables and disables models on a connection. It reaches no
// address and changes no key, so it needs no password.
func (h *Handler) handleSetModels(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	row, _, ok := h.connectionFor(w, r, userID)
	if !ok {
		return
	}
	var req setModelsRequest
	if err := handlerutils.DecodeJSON(r, &req); err != nil || len(req.Models) == 0 || len(req.Models) > maxListModels {
		badRequest(w, h.logger, "Send the models to enable or disable")
		return
	}
	err := h.store.SetModels(r.Context(), userID, row, req.Models)
	switch {
	case errors.Is(err, ErrInvalidModelID):
		badRequest(w, h.logger, err.Error())
		return
	case errors.Is(err, ErrDefaultModelInUse):
		h.respondDefaultInUse(w)
		return
	case err != nil:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to change the connection's models", err)
		return
	}
	enabled, err := h.store.withEnabled(r.Context(), row.ID, nil)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the connection's models", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, enabled)
}

// respondDefaultInUse answers a change that would take away the account's
// default model.
func (h *Handler) respondDefaultInUse(w http.ResponseWriter) {
	handlerutils.RespondWithError(w, h.logger, http.StatusConflict, models.ErrCodeDefaultModelInUse, ErrDefaultModelInUse.Error(), nil)
}
