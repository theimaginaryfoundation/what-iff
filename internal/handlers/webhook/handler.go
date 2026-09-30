package webhook

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type Handler struct {
	provider Provider
	agent    MessageAgent
	logger   *zap.Logger
}

func NewHandler(provider Provider, agent MessageAgent, logger *zap.Logger) *Handler {
	return &Handler{
		provider: provider,
		agent:    agent,
		logger:   logger,
	}
}

func (h *Handler) RegisterTokenRoutes(router *mux.Router) {
	tokenRouter := router.PathPrefix("/webhook-tokens").Subrouter()
	tokenRouter.HandleFunc("", h.CreateWebhookToken).Methods("POST")
	tokenRouter.HandleFunc("", h.ListWebhookTokens).Methods("GET")
	tokenRouter.HandleFunc("/{id}", h.RevokeWebhookToken).Methods("DELETE")
}

// RegisterWebhookRoutes mounts the webhook surface. The router it is given must already run
// WebhookAuthMiddleware; each route here additionally requires the scope that covers it, so a
// token minted to post messages cannot read, and one minted to read cannot post.
//
// The read routes share names with the session API where one exists (threads, messages, jobs), so
// a client moves between the two by changing the prefix and the credential.
func (h *Handler) RegisterWebhookRoutes(router *mux.Router) {
	write := middleware.RequireWebhookScope(models.WebhookScopeMessagesWrite, h.logger)
	read := middleware.RequireWebhookScope(models.WebhookScopeChatRead, h.logger)
	route := func(path string, scope func(http.Handler) http.Handler, fn http.HandlerFunc, method string) {
		router.Handle(path, scope(fn)).Methods(method)
	}

	route("/chat/{chatId}/messages", write, h.SendChatMessage, http.MethodPost)

	route("/chat", read, h.ListChats, http.MethodGet)
	route("/personality", read, h.ListPersonalities, http.MethodGet)
	route("/chat/{chatId}/messages", read, h.ListChatMessages, http.MethodGet)
	route("/chat/chat-message/{id}", read, h.GetChatMessage, http.MethodGet)
	route("/job/{id}", read, h.GetJob, http.MethodGet)
}
