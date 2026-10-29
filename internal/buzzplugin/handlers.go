package buzzplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Store is the storage the routes need (the datastore satisfies it).
type Store interface {
	CreateBuzzAgent(ctx context.Context, userID uuid.UUID, in models.BuzzAgentCreate) (*models.BuzzAgent, error)
	ListBuzzAgents(ctx context.Context, userID uuid.UUID) ([]models.BuzzAgent, error)
	GetBuzzAgent(ctx context.Context, userID, id uuid.UUID) (*models.BuzzAgent, error)
	GetBuzzAgentCredentials(ctx context.Context, userID, id uuid.UUID) (*models.BuzzAgentCredentials, error)
	SetBuzzAgentStatus(ctx context.Context, userID, id uuid.UUID, status models.BuzzAgentStatus) (*models.BuzzAgent, error)
	DeleteBuzzAgent(ctx context.Context, userID, id uuid.UUID) error

	CreateBuzzCommunity(ctx context.Context, userID, agentID uuid.UUID, in models.BuzzCommunityCreate) (*models.BuzzCommunity, error)
	ListBuzzCommunities(ctx context.Context, userID uuid.UUID) ([]models.BuzzCommunity, error)
	GetBuzzCommunity(ctx context.Context, userID, id uuid.UUID) (*models.BuzzCommunity, error)
	GetBuzzConnection(ctx context.Context, userID, id uuid.UUID) (*models.BuzzConnection, error)
	UpdateBuzzCommunity(ctx context.Context, userID, id uuid.UUID, p models.BuzzCommunityPatch) (*models.BuzzCommunity, error)
	DeleteBuzzCommunity(ctx context.Context, userID, id uuid.UUID) error

	CreateBuzzBinding(ctx context.Context, userID, communityID uuid.UUID, in models.BuzzBindingCreate) (*models.BuzzBinding, error)
	ListBuzzBindings(ctx context.Context, userID uuid.UUID) ([]models.BuzzBinding, error)
	ListBuzzBindingsForChat(ctx context.Context, userID, chatID uuid.UUID) ([]models.BuzzBinding, error)
	GetBuzzBinding(ctx context.Context, userID, id uuid.UUID) (*models.BuzzBinding, error)
	UpdateBuzzBinding(ctx context.Context, userID, id uuid.UUID, p models.BuzzBindingPatch) (*models.BuzzBinding, error)
	DeleteBuzzBinding(ctx context.Context, userID, id uuid.UUID) error

	ListBuzzLinksForChat(ctx context.Context, userID, chatID uuid.UUID, limit int) ([]models.BuzzMessageLink, error)
	SetBuzzPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID, source models.BuzzPendingPostSource) error
	ClearBuzzPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID) error
	ListBuzzPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error)

	GetPersonality(ctx context.Context, userID, id uuid.UUID) (*models.Personality, error)
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
	CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
	DeleteChat(ctx context.Context, userID, id uuid.UUID) error
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
}

// Relay is what the routes need from a community (*buzzrelay.Client satisfies it).
type Relay interface {
	buzzrelay.Relay
	Info(ctx context.Context, ep buzzrelay.Endpoint) (*buzzrelay.RelayInfo, error)
	JoinPolicy(ctx context.Context, ep buzzrelay.Endpoint) (*buzzrelay.JoinPolicy, error)
	ClaimInvite(ctx context.Context, ep buzzrelay.Endpoint, s buzzrelay.Signer, code, policyVersion string, ageConfirmed bool) (bool, error)
}

// Handler serves /api/buzz/... (behind auth).
type Handler struct {
	Store Store
	Relay Relay
	// Files is the object store, for uploading a persona's portrait with its profile. Nil
	// publishes the profile without a picture.
	Files  storage.FileStore
	Logger *zap.Logger
	// AllowPrivate accepts ws:// and http:// community addresses (local development).
	AllowPrivate bool
	// Changed is told when a community or binding changed, so the gateway reconciles now
	// instead of at its next interval. Nil does nothing.
	Changed func()
}

func (h *Handler) changed() {
	if h.Changed != nil {
		h.Changed()
	}
}

// Register mounts the routes on r (already prefixed with /buzz).
func (h *Handler) Register(r *mux.Router) {
	r.HandleFunc("/agents", h.listAgents).Methods(http.MethodGet)
	r.HandleFunc("/agents", h.createAgent).Methods(http.MethodPost)
	r.HandleFunc("/agents/{id}", h.updateAgent).Methods(http.MethodPatch)
	r.HandleFunc("/agents/{id}", h.deleteAgent).Methods(http.MethodDelete)
	r.HandleFunc("/agents/{id}/sync-profile", h.syncProfile).Methods(http.MethodPost)
	r.HandleFunc("/agents/{id}/communities", h.addCommunity).Methods(http.MethodPost)

	r.HandleFunc("/policy", h.previewPolicy).Methods(http.MethodGet)
	r.HandleFunc("/communities/{id}", h.updateCommunity).Methods(http.MethodPatch)
	r.HandleFunc("/communities/{id}", h.deleteCommunity).Methods(http.MethodDelete)
	r.HandleFunc("/communities/{id}/channels", h.listChannels).Methods(http.MethodGet)
	r.HandleFunc("/communities/{id}/channels/{channelId}/join", h.joinChannel).Methods(http.MethodPost)

	r.HandleFunc("/bindings", h.listBindings).Methods(http.MethodGet)
	r.HandleFunc("/bindings", h.createBinding).Methods(http.MethodPost)
	r.HandleFunc("/bindings/{id}", h.updateBinding).Methods(http.MethodPatch)
	r.HandleFunc("/bindings/{id}", h.deleteBinding).Methods(http.MethodDelete)

	r.HandleFunc("/chats/{chatId}", h.chatState).Methods(http.MethodGet)
	r.HandleFunc("/chats/{chatId}/pending/{bindingId}", h.setPending).Methods(http.MethodPut)
	r.HandleFunc("/chats/{chatId}/pending/{bindingId}", h.clearPending).Methods(http.MethodDelete)
}

// AgentResponse is a persona's Buzz identity with the communities it is in.
type AgentResponse struct {
	models.BuzzAgent
	PersonaName string                 `json:"persona_name"`
	Communities []models.BuzzCommunity `json:"communities"`
}

func (h *Handler) listAgents(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	agents, err := h.Store.ListBuzzAgents(r.Context(), userID)
	if err != nil {
		h.fail(w, err, "Failed to list Buzz identities")
		return
	}
	communities, err := h.Store.ListBuzzCommunities(r.Context(), userID)
	if err != nil {
		h.fail(w, err, "Failed to list Buzz communities")
		return
	}
	byAgent := map[uuid.UUID][]models.BuzzCommunity{}
	for _, c := range communities {
		byAgent[c.AgentID] = append(byAgent[c.AgentID], c)
	}
	out := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		out = append(out, h.agentResponse(r.Context(), userID, a, byAgent[a.ID]))
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, out)
}

func (h *Handler) agentResponse(ctx context.Context, userID uuid.UUID, a models.BuzzAgent, communities []models.BuzzCommunity) AgentResponse {
	resp := AgentResponse{BuzzAgent: a, Communities: communities}
	if resp.Communities == nil {
		resp.Communities = []models.BuzzCommunity{}
	}
	if p, err := h.Store.GetPersonality(ctx, userID, a.PersonalityID); err == nil && p != nil {
		resp.PersonaName = p.Name
	}
	return resp
}

type createAgentRequest struct {
	PersonalityID uuid.UUID `json:"personality_id"`
}

// createAgent gives a persona its Buzz identity: a new keypair, whose secret stays on the server.
func (h *Handler) createAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	var req createAgentRequest
	if !h.decode(w, r, &req) {
		return
	}
	if req.PersonalityID == uuid.Nil {
		h.bad(w, "personality_id is required")
		return
	}
	keys, err := nostr.GenerateKeys()
	if err != nil {
		h.fail(w, err, "Failed to create a Buzz identity")
		return
	}
	agent, err := h.Store.CreateBuzzAgent(r.Context(), userID, models.BuzzAgentCreate{
		PersonalityID: req.PersonalityID, PubKey: keys.PublicKey(), SecretKey: keys.SecretHex(),
	})
	if err != nil {
		h.fail(w, err, "Failed to save the Buzz identity")
		return
	}
	h.Logger.Info("buzz identity created", zap.String("agent_id", agent.ID.String()), zap.String("user_id", userID.String()))
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusCreated, h.agentResponse(r.Context(), userID, *agent, nil))
}

type updateAgentRequest struct {
	// Enabled pauses (false) or resumes (true) the persona in every community.
	Enabled *bool `json:"enabled"`
}

func (h *Handler) updateAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var req updateAgentRequest
	if !h.decode(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		h.bad(w, "nothing to change")
		return
	}
	status := models.BuzzAgentDisabled
	if *req.Enabled {
		status = models.BuzzAgentActive
	}
	agent, err := h.Store.SetBuzzAgentStatus(r.Context(), userID, id, status)
	if err != nil {
		h.fail(w, err, "Failed to update the Buzz identity")
		return
	}
	h.changed()
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, h.agentResponse(r.Context(), userID, *agent, h.communitiesOf(r.Context(), userID, id)))
}

func (h *Handler) communitiesOf(ctx context.Context, userID, agentID uuid.UUID) []models.BuzzCommunity {
	all, err := h.Store.ListBuzzCommunities(ctx, userID)
	if err != nil {
		return nil
	}
	var out []models.BuzzCommunity
	for _, c := range all {
		if c.AgentID == agentID {
			out = append(out, c)
		}
	}
	return out
}

func (h *Handler) deleteAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.Store.DeleteBuzzAgent(r.Context(), userID, id); err != nil {
		h.fail(w, err, "Failed to remove the Buzz identity")
		return
	}
	h.changed()
	w.WriteHeader(http.StatusNoContent)
}

// syncProfile republishes the persona's name and portrait to every community it is in.
func (h *Handler) syncProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := h.Store.GetBuzzAgent(r.Context(), userID, id); err != nil {
		h.fail(w, err, "Failed to load the Buzz identity")
		return
	}
	published, failed := 0, []string{}
	for _, c := range h.communitiesOf(r.Context(), userID, id) {
		if c.Status == models.BuzzCommunityDisabled {
			continue
		}
		conn, err := h.Store.GetBuzzConnection(r.Context(), userID, c.ID)
		if err != nil {
			failed = append(failed, c.RelayURL)
			continue
		}
		if err := h.publishProfile(r.Context(), userID, conn); err != nil {
			h.Logger.Warn("buzz profile publish failed", zap.String("community_id", c.ID.String()), zap.Error(err))
			failed = append(failed, c.RelayURL)
			continue
		}
		published++
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, map[string]any{"published": published, "failed": failed})
}

// --- communities ---

type addCommunityRequest struct {
	// RelayURL is the community's address, or an invite link to it.
	RelayURL string `json:"relay_url"`
	// Invite is an invite link or code, when RelayURL is not already one.
	Invite string `json:"invite"`
	// Attestation is the owner's NIP-OA attestation for the persona's key (the JSON auth tag).
	Attestation string `json:"attestation"`
	// AcceptTermsVersion is the community's join terms version the owner read and accepted
	// for the persona. Required to claim an invite when the community has terms.
	AcceptTermsVersion string `json:"accept_terms_version"`
	// ConfirmAge is the owner's confirmation of the community's age requirement, when it
	// has one (age_attestation_required).
	ConfirmAge bool `json:"confirm_age"`
}

// termsRequiredMsg is the 409 message for an invite claim whose community has join terms the
// owner has not accepted (yet, or in this version). The envelope's details carry the terms.
const termsRequiredMsg = "This community has join terms. Read them, and accept them for the persona to join."

// addCommunity joins a persona to a community: reads the community, verifies an attestation,
// claims an invite (after the owner accepted the community's terms for the persona), records
// the community, publishes the persona's profile and checks whether it is let in.
func (h *Handler) addCommunity(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	agentID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var req addCommunityRequest
	if !h.decode(w, r, &req) {
		return
	}
	creds, err := h.Store.GetBuzzAgentCredentials(r.Context(), userID, agentID)
	if err != nil {
		h.fail(w, err, "Failed to load the Buzz identity")
		return
	}
	ep, code, err := buzzrelay.ParseEndpoint(req.RelayURL, h.AllowPrivate)
	if err != nil {
		h.bad(w, err.Error())
		return
	}
	if inv := strings.TrimSpace(req.Invite); inv != "" {
		code = inviteCode(inv)
	}
	// Before anything is sent to the community: re-adding one the persona is already in would
	// otherwise use up a (possibly single-use) invite and then fail on the unique constraint.
	if exists, err := h.inCommunity(r.Context(), userID, agentID, ep.WS); err != nil {
		h.fail(w, err, "Failed to load the persona's communities")
		return
	} else if exists {
		h.fail(w, datastore.ErrBuzzCommunityExists, "")
		return
	}
	info, ok := h.readInfo(w, r, ep)
	if !ok {
		return
	}
	signer, err := signerOf(creds, "")
	if err != nil {
		h.fail(w, err, "Failed to load the Buzz identity")
		return
	}
	var authTag, ownerPub *string
	if strings.TrimSpace(req.Attestation) != "" {
		att, ok := h.verifyAttestation(w, req.Attestation, creds.PubKey)
		if !ok {
			return
		}
		tag, owner := att.JSON(), att.OwnerPubKey
		authTag, ownerPub = &tag, &owner
		signer.Attestation = att
	}
	var policyVersion *string
	if code != "" {
		v, ok := h.claim(w, r, ep, signer, code, req.AcceptTermsVersion, req.ConfirmAge)
		if !ok {
			return
		}
		if v != "" {
			policyVersion = &v
		}
	}
	community, err := h.Store.CreateBuzzCommunity(r.Context(), userID, agentID, models.BuzzCommunityCreate{
		RelayURL: ep.WS, Name: communityName(info, ep), RelayPubKey: info.Self,
		AuthTag: authTag, OwnerPubKey: ownerPub, PolicyVersion: policyVersion,
		Status: models.BuzzCommunityNeedsAccess,
	})
	if err != nil {
		h.fail(w, err, "Failed to save the community")
		return
	}
	community = h.recheck(r.Context(), userID, community.ID, true)
	h.changed()
	h.Logger.Info("buzz community added", zap.String("community_id", community.ID.String()), zap.String("status", string(community.Status)))
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusCreated, community)
}

// inCommunity reports whether the persona already has the community at relayURL (normalised).
func (h *Handler) inCommunity(ctx context.Context, userID, agentID uuid.UUID, relayURL string) (bool, error) {
	communities, err := h.Store.ListBuzzCommunities(ctx, userID)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(communities, func(c models.BuzzCommunity) bool {
		return c.AgentID == agentID && c.RelayURL == relayURL
	}), nil
}

// readInfo reads a community's NIP-11 document and checks it is Buzz. When ok is false the
// response has been written.
func (h *Handler) readInfo(w http.ResponseWriter, r *http.Request, ep buzzrelay.Endpoint) (*buzzrelay.RelayInfo, bool) {
	info, err := h.Relay.Info(r.Context(), ep)
	if err != nil {
		h.Logger.Info("buzz community unreachable", zap.String("relay", ep.WS), zap.Error(err))
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet,
			"That community could not be reached. Check the address and try again.", nil)
		return nil, false
	}
	if !info.IsBuzz() {
		h.bad(w, "That address is a Nostr relay, but not a Buzz community.")
		return nil, false
	}
	return info, true
}

func (h *Handler) verifyAttestation(w http.ResponseWriter, raw, agentPub string) (*nostr.Attestation, bool) {
	att, err := nostr.ParseAttestation(raw)
	if err != nil {
		h.bad(w, err.Error())
		return nil, false
	}
	if err := att.Verify(agentPub); err != nil {
		h.bad(w, "That attestation is not signed for this persona's key. Create it for "+npubOf(agentPub)+".")
		return nil, false
	}
	return att, true
}

// claim joins the community with an invite as the persona. When the community has join terms,
// the owner must have accepted this version (and confirmed the age requirement, when there is
// one); otherwise the 409 (code buzz_terms_required) carries the terms to show in its details.
// It returns the terms version accepted ("" when there are none). When ok is false the
// response has been written.
func (h *Handler) claim(w http.ResponseWriter, r *http.Request, ep buzzrelay.Endpoint, signer buzzrelay.Signer, code, accepted string, ageConfirmed bool) (string, bool) {
	policy, err := h.Relay.JoinPolicy(r.Context(), ep)
	if err != nil {
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet,
			"The community's join terms could not be read. Try again.", nil)
		return "", false
	}
	version := ""
	if policy != nil && policy.Version != "" {
		if strings.TrimSpace(accepted) != policy.Version || (policy.AgeAttestationRequired && !ageConfirmed) {
			handlerutils.RespondWithErrorDetails(w, h.Logger, http.StatusConflict, models.ErrCodeBuzzTermsRequired,
				termsRequiredMsg, map[string]any{"policy": policy}, nil)
			return "", false
		}
		version = policy.Version
	}
	if _, err := h.Relay.ClaimInvite(r.Context(), ep, signer, code, version, ageConfirmed); err != nil {
		h.Logger.Info("buzz invite claim failed", zap.String("relay", ep.WS), zap.Error(err))
		h.bad(w, inviteError(err))
		return "", false
	}
	return version, true
}

// inviteError says why an invite was refused, in the owner's terms.
func inviteError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "invite_expired"):
		return "That invite has expired. Ask for a new one."
	case strings.Contains(msg, "invite_exhausted"):
		return "That invite has been used up. Ask for a new one."
	case strings.Contains(msg, "too many"):
		return "Too many tries; wait a minute and try again."
	case strings.Contains(msg, "join_policy"):
		return "The community's terms changed. Read and accept them again."
	case errors.Is(err, buzzrelay.ErrUnreachable):
		return "The community could not be reached. Try again."
	}
	return "That invite was not accepted. Check it, or ask for a new one."
}

// inviteCode accepts an invite as a full link or a bare code.
func inviteCode(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "/invite/"); i >= 0 {
		s = s[i+len("/invite/"):]
	}
	return strings.Trim(s, "/ ")
}

func communityName(info *buzzrelay.RelayInfo, ep buzzrelay.Endpoint) string {
	if n := strings.TrimSpace(info.Name); n != "" && n != "Buzz Relay" {
		return n
	}
	return ep.Host()
}

// recheck publishes the persona's profile and probes whether the community lets it in,
// recording the result on the community. The gateway keeps the state current afterwards;
// this gives the owner an answer straight away. withProfile publishes the profile first.
func (h *Handler) recheck(ctx context.Context, userID, communityID uuid.UUID, withProfile bool) *models.BuzzCommunity {
	conn, err := h.Store.GetBuzzConnection(ctx, userID, communityID)
	if err != nil {
		c, _ := h.Store.GetBuzzCommunity(ctx, userID, communityID)
		return c
	}
	status, detail := h.probe(ctx, conn)
	if withProfile && status == models.BuzzCommunityActive {
		if err := h.publishProfile(ctx, userID, conn); err != nil {
			h.Logger.Warn("buzz profile publish failed", zap.Error(err))
		}
	}
	patch := models.BuzzCommunityPatch{Status: &status, LastError: &detail, ConnectedNow: status == models.BuzzCommunityActive}
	if c, err := h.Store.UpdateBuzzCommunity(ctx, userID, communityID, patch); err == nil {
		return c
	}
	c, _ := h.Store.GetBuzzCommunity(ctx, userID, communityID)
	return c
}

// probe asks the community something only members may read (the persona's own profile).
func (h *Handler) probe(ctx context.Context, conn *models.BuzzConnection) (models.BuzzCommunityStatus, string) {
	ep, signer, err := endpointSigner(conn)
	if err != nil {
		return models.BuzzCommunityError, "The stored identity could not be used."
	}
	_, err = h.Relay.Query(ctx, ep, signer, buzzrelay.Filter{"kinds": []int{buzzrelay.KindProfile}, "authors": []string{conn.Agent.PubKey}, "limit": 1})
	switch {
	case err == nil:
		return models.BuzzCommunityActive, ""
	case errors.Is(err, buzzrelay.ErrNoAccess):
		return models.BuzzCommunityNeedsAccess, "Not a member yet. Ask an admin to add " + npubOf(conn.Agent.PubKey) +
			", paste an invite, or give the persona your attestation."
	default:
		return models.BuzzCommunityError, "The community could not be reached."
	}
}

// publishProfile publishes the persona's kind 0 profile (name, portrait, bot flag) to one
// community. The portrait is uploaded to the community's media store when it can be.
func (h *Handler) publishProfile(ctx context.Context, userID uuid.UUID, conn *models.BuzzConnection) error {
	ep, signer, err := endpointSigner(conn)
	if err != nil {
		return err
	}
	persona, err := h.Store.GetPersonality(ctx, userID, conn.Agent.PersonalityID)
	if err != nil {
		return err
	}
	profile := buzzrelay.Profile{
		Name: persona.Name, DisplayName: persona.Name, Bot: true,
		About: "A persona on What Iff.",
	}
	if picture := h.portraitURL(ctx, userID, ep, signer, persona); picture != "" {
		profile.Picture = picture
	}
	ev, err := buzzrelay.ProfileEvent(signer, profile)
	if err != nil {
		return err
	}
	return h.Relay.Publish(ctx, ep, signer, ev)
}

// maxPortraitBytes bounds the portrait uploaded with a profile.
const maxPortraitBytes = 10 << 20

func (h *Handler) portraitURL(ctx context.Context, userID uuid.UUID, ep buzzrelay.Endpoint, signer buzzrelay.Signer, p *models.Personality) string {
	if h.Files == nil || p.CoverImageID == nil {
		return ""
	}
	att, err := h.Store.GetFileAttachment(ctx, userID, *p.CoverImageID)
	if err != nil || att == nil || att.S3Key == "" {
		return ""
	}
	data, err := h.Files.DownloadFile(ctx, att.S3Key)
	if err != nil || len(data) == 0 || len(data) > maxPortraitBytes {
		return ""
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return ""
	}
	blob, err := h.Relay.Upload(ctx, ep, signer, att.Name, mime, data)
	if err != nil {
		h.Logger.Info("buzz portrait upload failed", zap.Error(err))
		return ""
	}
	return blob.URL
}

// previewPolicy reads a community's join terms before the persona joins, so the app can show
// them with the invite (GET /policy?relay_url=...).
func (h *Handler) previewPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.user(w, r); !ok {
		return
	}
	ep, _, err := buzzrelay.ParseEndpoint(r.URL.Query().Get("relay_url"), h.AllowPrivate)
	if err != nil {
		h.bad(w, err.Error())
		return
	}
	info, ok := h.readInfo(w, r, ep)
	if !ok {
		return
	}
	policy, err := h.Relay.JoinPolicy(r.Context(), ep)
	if err != nil {
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet,
			"The community's join terms could not be read. Try again.", nil)
		return
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, map[string]any{
		"name": communityName(info, ep), "relay_url": ep.WS, "policy": policy,
	})
}

type updateCommunityRequest struct {
	// Enabled pauses (false) or resumes (true) the persona in this community.
	Enabled *bool `json:"enabled"`
	// Attestation replaces the owner attestation; "" removes it.
	Attestation *string `json:"attestation"`
	// Invite claims an invite now (with AcceptTermsVersion when the community has terms).
	Invite             string `json:"invite"`
	AcceptTermsVersion string `json:"accept_terms_version"`
	ConfirmAge         bool   `json:"confirm_age"`
	// Recheck probes access again (after an admin added the persona, say).
	Recheck bool `json:"recheck"`
}

func (h *Handler) updateCommunity(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var req updateCommunityRequest
	if !h.decode(w, r, &req) {
		return
	}
	conn, err := h.Store.GetBuzzConnection(r.Context(), userID, id)
	if err != nil {
		h.fail(w, err, "Failed to load the community")
		return
	}
	patch := models.BuzzCommunityPatch{}
	if req.Attestation != nil {
		if strings.TrimSpace(*req.Attestation) == "" {
			clear := ""
			patch.AuthTag, patch.OwnerPubKey = &clear, &clear
		} else {
			att, ok := h.verifyAttestation(w, *req.Attestation, conn.Agent.PubKey)
			if !ok {
				return
			}
			tag, owner := att.JSON(), att.OwnerPubKey
			patch.AuthTag, patch.OwnerPubKey = &tag, &owner
		}
	}
	if code := inviteCode(req.Invite); code != "" {
		ep, signer, err := endpointSigner(conn)
		if err != nil {
			h.fail(w, err, "Failed to load the community")
			return
		}
		version, ok := h.claim(w, r, ep, signer, code, req.AcceptTermsVersion, req.ConfirmAge)
		if !ok {
			return
		}
		if version != "" {
			patch.PolicyVersion = &version
		}
	}
	if req.Enabled != nil {
		st := models.BuzzCommunityDisabled
		if *req.Enabled {
			st = models.BuzzCommunityNeedsAccess // the probe below, then the gateway, set the real state
		}
		patch.Status = &st
	}
	community, err := h.Store.UpdateBuzzCommunity(r.Context(), userID, id, patch)
	if err != nil {
		h.fail(w, err, "Failed to update the community")
		return
	}
	if community.Status != models.BuzzCommunityDisabled && (req.Recheck || req.Invite != "" || req.Attestation != nil || req.Enabled != nil) {
		community = h.recheck(r.Context(), userID, id, true)
	}
	h.changed()
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, community)
}

func (h *Handler) deleteCommunity(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.Store.DeleteBuzzCommunity(r.Context(), userID, id); err != nil {
		h.fail(w, err, "Failed to remove the community")
		return
	}
	h.changed()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	conn, err := h.Store.GetBuzzConnection(r.Context(), userID, id)
	if err != nil {
		h.fail(w, err, "Failed to load the community")
		return
	}
	ep, signer, err := endpointSigner(conn)
	if err != nil {
		h.fail(w, err, "Failed to load the community")
		return
	}
	channels, err := buzzrelay.ListChannels(r.Context(), h.Relay, ep, signer, conn.Community.RelayPubKey)
	if err != nil {
		h.relayFailed(w, err)
		return
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, channels)
}

// joinChannel asks to join a public channel as the persona (NIP-29 join request).
func (h *Handler) joinChannel(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	channelID := strings.TrimSpace(mux.Vars(r)["channelId"])
	if _, err := uuid.Parse(channelID); err != nil {
		h.bad(w, "invalid channelId")
		return
	}
	conn, err := h.Store.GetBuzzConnection(r.Context(), userID, id)
	if err != nil {
		h.fail(w, err, "Failed to load the community")
		return
	}
	ep, signer, err := endpointSigner(conn)
	if err != nil {
		h.fail(w, err, "Failed to load the community")
		return
	}
	ev, err := buzzrelay.JoinEvent(signer, channelID)
	if err != nil {
		h.fail(w, err, "Failed to join the channel")
		return
	}
	if err := h.Relay.Publish(r.Context(), ep, signer, ev); err != nil {
		h.relayFailed(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- bindings ---

func (h *Handler) listBindings(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	bindings, err := h.Store.ListBuzzBindings(r.Context(), userID)
	if err != nil {
		h.fail(w, err, "Failed to list Buzz channels")
		return
	}
	h.markSandboxed(r.Context(), userID, bindings)
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, bindings)
}

// unrestrictedBindMsg is the 400 for binding a thread that is not sandboxed.
const unrestrictedBindMsg = "That thread is not sandboxed, so anyone allowed to mention the persona could use your memories, " +
	"other conversations, files and scratchpad. Sandbox the thread first, or confirm with allow_unrestricted."

// bindableChat decides whether chatID may be bound to a Buzz channel (or a binding repointed to
// it), as for Discord: a sandboxed thread always may; any other only with the owner's explicit
// acknowledgement. record is the value to store as allow_unrestricted.
func (h *Handler) bindableChat(w http.ResponseWriter, r *http.Request, userID, chatID uuid.UUID, acknowledged bool) (record, ok bool) {
	chat, err := h.Store.GetChat(r.Context(), userID, chatID)
	if err != nil {
		h.fail(w, err, "Failed to load the thread")
		return false, false
	}
	if relaykit.RelayThreadOpenWithoutAcknowledgement(chat) {
		return false, true
	}
	if !acknowledged {
		h.bad(w, unrestrictedBindMsg)
		return false, false
	}
	return true, true
}

// markSandboxed fills each binding's computed ChatSandboxed from its thread's current state.
func (h *Handler) markSandboxed(ctx context.Context, userID uuid.UUID, bindings []models.BuzzBinding) {
	seen := map[uuid.UUID]*bool{}
	for i := range bindings {
		id := bindings[i].ChatID
		v, ok := seen[id]
		if !ok {
			if chat, err := h.Store.GetChat(ctx, userID, id); err == nil && chat != nil {
				r := relaykit.RelayThreadOpenWithoutAcknowledgement(chat)
				v = &r
			}
			seen[id] = v
		}
		bindings[i].ChatSandboxed = v
	}
}

func (h *Handler) markOne(ctx context.Context, userID uuid.UUID, b *models.BuzzBinding) {
	bs := []models.BuzzBinding{*b}
	h.markSandboxed(ctx, userID, bs)
	b.ChatSandboxed = bs[0].ChatSandboxed
}

type createBindingRequest struct {
	CommunityID uuid.UUID `json:"community_id"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	// ChatID binds an existing thread; when nil a new sandboxed relay thread is created for the
	// persona.
	ChatID         *uuid.UUID `json:"chat_id"`
	InboundEnabled *bool      `json:"inbound_enabled"`
	AllowPubKeys   []string   `json:"allow_pubkeys"`
	DenyPubKeys    []string   `json:"deny_pubkeys"`
	AnswerAgents   bool       `json:"answer_agents"`
	// AllowUnrestricted acknowledges binding an existing thread that is not sandboxed.
	AllowUnrestricted bool `json:"allow_unrestricted"`
}

func (h *Handler) createBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	var req createBindingRequest
	if !h.decode(w, r, &req) {
		return
	}
	channelID := strings.TrimSpace(req.ChannelID)
	if req.CommunityID == uuid.Nil || channelID == "" {
		h.bad(w, "community_id and channel_id are required")
		return
	}
	allow, ok := h.pubkeys(w, req.AllowPubKeys)
	if !ok {
		return
	}
	deny, ok := h.pubkeys(w, req.DenyPubKeys)
	if !ok {
		return
	}
	conn, err := h.Store.GetBuzzConnection(r.Context(), userID, req.CommunityID)
	if err != nil {
		h.fail(w, err, "Failed to load the community")
		return
	}
	chatID := uuid.Nil
	allowUnrestricted := false
	if req.ChatID != nil {
		chatID = *req.ChatID
		if allowUnrestricted, ok = h.bindableChat(w, r, userID, chatID, req.AllowUnrestricted); !ok {
			return
		}
	} else {
		// A relay thread is a public surface, so it is created sandboxed, exactly as for
		// Discord: it reads only itself, writes only Chat-scoped memories, starts with the
		// sandbox's default-off tools, and has no MCP connectors (CreateChat attaches none).
		chat, err := h.Store.CreateChat(r.Context(), userID, models.Chat{
			Name:          relayThreadName(conn.Community, req.ChannelName),
			PersonalityID: conn.Agent.PersonalityID,
			ContextScope:  models.ContextScopeSandbox,
		})
		if err != nil {
			h.fail(w, err, "Failed to create the relay thread")
			return
		}
		chatID = chat.ID
	}
	inbound := true
	if req.InboundEnabled != nil {
		inbound = *req.InboundEnabled
	}
	binding, err := h.Store.CreateBuzzBinding(r.Context(), userID, req.CommunityID, models.BuzzBindingCreate{
		ChatID:            chatID,
		ChannelID:         channelID,
		ChannelName:       strings.TrimPrefix(strings.TrimSpace(req.ChannelName), "#"),
		InboundEnabled:    inbound,
		AllowPubKeys:      allow,
		DenyPubKeys:       deny,
		AnswerAgents:      req.AnswerAgents,
		AllowUnrestricted: allowUnrestricted,
	})
	if err != nil {
		if req.ChatID == nil {
			if derr := h.Store.DeleteChat(r.Context(), userID, chatID); derr != nil {
				h.Logger.Warn("buzz: could not remove the unused relay thread", zap.Error(derr))
			}
		}
		h.fail(w, err, "Failed to connect the channel")
		return
	}
	h.changed()
	h.markOne(r.Context(), userID, binding)
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusCreated, binding)
}

func relayThreadName(c models.BuzzCommunity, channel string) string {
	channel = strings.TrimPrefix(strings.TrimSpace(channel), "#")
	if channel == "" {
		return "Buzz · " + c.Name
	}
	return "Buzz · #" + channel
}

type updateBindingRequest struct {
	ChatID            *uuid.UUID `json:"chat_id"`
	InboundEnabled    *bool      `json:"inbound_enabled"`
	AllowPubKeys      *[]string  `json:"allow_pubkeys"`
	DenyPubKeys       *[]string  `json:"deny_pubkeys"`
	AnswerAgents      *bool      `json:"answer_agents"`
	ChannelName       *string    `json:"channel_name"`
	AllowUnrestricted *bool      `json:"allow_unrestricted"`
	// Reactivate clears a broken status (after fixing the persona's access).
	Reactivate bool `json:"reactivate"`
}

func (h *Handler) updateBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var req updateBindingRequest
	if !h.decode(w, r, &req) {
		return
	}
	p := models.BuzzBindingPatch{ChatID: req.ChatID, InboundEnabled: req.InboundEnabled, ChannelName: req.ChannelName, AnswerAgents: req.AnswerAgents}
	if req.AllowPubKeys != nil {
		keys, ok := h.pubkeys(w, *req.AllowPubKeys)
		if !ok {
			return
		}
		p.AllowPubKeys = &keys
	}
	if req.DenyPubKeys != nil {
		keys, ok := h.pubkeys(w, *req.DenyPubKeys)
		if !ok {
			return
		}
		p.DenyPubKeys = &keys
	}
	if req.Reactivate {
		st, clear := models.BuzzBindingActive, ""
		p.Status, p.LastError = &st, &clear
	}
	if req.ChatID != nil {
		// Repointing: the acknowledgement belongs to the thread it was given for.
		ack := req.AllowUnrestricted != nil && *req.AllowUnrestricted
		record, ok := h.bindableChat(w, r, userID, *req.ChatID, ack)
		if !ok {
			return
		}
		p.AllowUnrestricted = &record
	} else if req.AllowUnrestricted != nil {
		current, err := h.Store.GetBuzzBinding(r.Context(), userID, id)
		if err != nil {
			h.fail(w, err, "Failed to load the channel")
			return
		}
		record := false
		if *req.AllowUnrestricted {
			chat, err := h.Store.GetChat(r.Context(), userID, current.ChatID)
			if err != nil {
				h.fail(w, err, "Failed to load the thread")
				return
			}
			record = !relaykit.RelayThreadOpenWithoutAcknowledgement(chat)
		}
		p.AllowUnrestricted = &record
	}
	binding, err := h.Store.UpdateBuzzBinding(r.Context(), userID, id, p)
	if err != nil {
		h.fail(w, err, "Failed to update the channel")
		return
	}
	h.changed()
	h.markOne(r.Context(), userID, binding)
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, binding)
}

func (h *Handler) deleteBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.Store.DeleteBuzzBinding(r.Context(), userID, id); err != nil {
		h.fail(w, err, "Failed to disconnect the channel")
		return
	}
	h.changed()
	w.WriteHeader(http.StatusNoContent)
}

// ChatState is what a thread needs to show its Buzz side.
type ChatState struct {
	Bindings       []models.BuzzBinding     `json:"bindings"`
	PendingBinding []uuid.UUID              `json:"pending_binding_ids"`
	Links          []models.BuzzMessageLink `json:"links"`
}

func (h *Handler) chatState(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	chatID, ok := h.pathID(w, r, "chatId")
	if !ok {
		return
	}
	bindings, err := h.Store.ListBuzzBindingsForChat(r.Context(), userID, chatID)
	if err != nil {
		h.fail(w, err, "Failed to load Buzz state")
		return
	}
	h.markSandboxed(r.Context(), userID, bindings)
	state := ChatState{Bindings: bindings, PendingBinding: []uuid.UUID{}, Links: []models.BuzzMessageLink{}}
	if len(bindings) > 0 {
		if state.PendingBinding, err = h.Store.ListBuzzPendingPosts(r.Context(), userID, chatID); err != nil {
			h.fail(w, err, "Failed to load Buzz state")
			return
		}
		if state.Links, err = h.Store.ListBuzzLinksForChat(r.Context(), userID, chatID, 200); err != nil {
			h.fail(w, err, "Failed to load Buzz state")
			return
		}
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, state)
}

func (h *Handler) setPending(w http.ResponseWriter, r *http.Request)   { h.pending(w, r, true) }
func (h *Handler) clearPending(w http.ResponseWriter, r *http.Request) { h.pending(w, r, false) }

func (h *Handler) pending(w http.ResponseWriter, r *http.Request, set bool) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	chatID, ok := h.pathID(w, r, "chatId")
	if !ok {
		return
	}
	bindingID, ok := h.pathID(w, r, "bindingId")
	if !ok {
		return
	}
	if _, err := h.Store.GetChat(r.Context(), userID, chatID); err != nil {
		h.fail(w, err, "Failed to load the thread")
		return
	}
	var err error
	if set {
		err = h.Store.SetBuzzPendingPost(r.Context(), userID, bindingID, chatID, models.BuzzPostFromComposer)
	} else {
		err = h.Store.ClearBuzzPendingPost(r.Context(), userID, bindingID, chatID)
	}
	if err != nil {
		h.fail(w, err, "Failed to update the post setting")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func signerOf(creds *models.BuzzAgentCredentials, authTag string) (buzzrelay.Signer, error) {
	return buzzrelay.SignerFor(*creds, authTag)
}

func endpointSigner(conn *models.BuzzConnection) (buzzrelay.Endpoint, buzzrelay.Signer, error) {
	ep, err := buzzrelay.EndpointFor(conn.Community)
	if err != nil {
		return buzzrelay.Endpoint{}, buzzrelay.Signer{}, err
	}
	s, err := buzzrelay.SignerFor(conn.Agent, conn.AuthTag)
	return ep, s, err
}

func npubOf(pubkey string) string {
	n, err := nostr.EncodeNPub(pubkey)
	if err != nil {
		return pubkey
	}
	return n
}

// pubkeys normalises a list of pubkeys (hex or npub), de-duplicated. A bad entry is a 400
// naming it, rather than being dropped: an allow or deny list that silently lost an entry
// would not do what the owner asked.
func (h *Handler) pubkeys(w http.ResponseWriter, in []string) ([]string, bool) {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range in {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		key, err := nostr.NormalizePubKey(raw)
		if err != nil {
			h.bad(w, "Not a Buzz public key (npub or hex): "+strings.TrimSpace(raw))
			return nil, false
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out, true
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.Logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
	}
	return id, ok
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(mux.Vars(r)[name])
	if err != nil {
		h.bad(w, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		h.bad(w, "invalid request body")
		return false
	}
	return true
}

func (h *Handler) bad(w http.ResponseWriter, msg string) {
	handlerutils.RespondWithError(w, h.Logger, http.StatusBadRequest, handlerutils.CodeNotSet, msg, nil)
}

// fail maps storage errors to responses: not-found (including another account's rows) is 404,
// conflicts are 409, anything else 500.
func (h *Handler) fail(w http.ResponseWriter, err error, msg string) {
	switch {
	case errors.Is(err, datastore.ErrBuzzAgentNotFound), errors.Is(err, datastore.ErrBuzzCommunityNotFound),
		errors.Is(err, datastore.ErrBuzzBindingNotFound), errors.Is(err, datastore.ErrChatNotFound),
		errors.Is(err, datastore.ErrPersonalityNotFound):
		handlerutils.RespondWithError(w, h.Logger, http.StatusNotFound, handlerutils.CodeNotSet, "Not found", nil)
	case errors.Is(err, datastore.ErrBuzzAgentExists):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet,
			"That persona is already connected to Buzz.", nil)
	case errors.Is(err, datastore.ErrBuzzCommunityExists):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet,
			"That persona is already in that community.", nil)
	case errors.Is(err, datastore.ErrBuzzBindingExists):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet,
			"That channel is already connected for this persona.", nil)
	case errors.Is(err, datastore.ErrBuzzBindingPaused):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet,
			"That channel is paused or needs attention, so nothing is posted there. Turn it back on first.", nil)
	default:
		handlerutils.RespondWithError(w, h.Logger, http.StatusInternalServerError, handlerutils.CodeNotSet, msg, err)
	}
}

func (h *Handler) relayFailed(w http.ResponseWriter, err error) {
	var rejected *buzzrelay.RejectedError
	switch {
	case errors.Is(err, buzzrelay.ErrNoAccess):
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet,
			"The community has not let this persona in yet.", nil)
	case errors.As(err, &rejected):
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet,
			"Buzz refused that: "+rejected.Reason, nil)
	default:
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet,
			"The community could not be reached. Try again.", err)
	}
}
