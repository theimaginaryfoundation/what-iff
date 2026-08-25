package webhook

import (
	"github.com/gorilla/mux"
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

func (h *Handler) RegisterWebhookRoutes(router *mux.Router) {
	router.HandleFunc("/chat/{chatId}/messages", h.SendChatMessage).Methods("POST")
}
