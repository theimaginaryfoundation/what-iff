package chat

import (
	"context"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// MessageAgent is the minimal agent surface needed by chat handlers.
// Keeping this as an interface makes the handler unit-testable.
type MessageAgent interface {
	HandleUserMessage(ctx context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error)
	RetryUserChatMessage(ctx context.Context, chatID, messageID uuid.UUID) (*models.ChatMessageResponse, error)
}

// WelcomeMessageAgent is the minimal async prompt surface used for welcome generation.
type WelcomeMessageAgent interface {
	HandleWelcomeMessagePromptAsync(ctx context.Context, chatID uuid.UUID, prompt string, modelOverrideID *uuid.UUID, personalityOverrideID *uuid.UUID) (*models.ChatMessageResponse, error)
}

// HandlerConfig configures chat handler behavior.
type HandlerConfig struct {
	RequireBilling bool
}

// Handler handles assistant-related API requests
type Handler struct {
	ds     Store
	logger *zap.Logger
	agent  *agent.Agent
	// messageAgent is used by CreateChatMessage; it defaults to agent but can be overridden for tests.
	messageAgent MessageAgent
	welcomeAgent WelcomeMessageAgent
	cfg          HandlerConfig
}

// NewHandler creates a new assistant handler instance
func NewHandler(ds Store, logger *zap.Logger, agent *agent.Agent, cfg HandlerConfig) *Handler {
	return &Handler{
		ds:           ds,
		logger:       logger,
		agent:        agent,
		messageAgent: agent,
		welcomeAgent: agent,
		cfg:          cfg,
	}
}

// RegisterRoutes registers all chat-related routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	chatRouter := router.PathPrefix("/chat").Subrouter()

	// Specific routes first (before routes with path variables)
	chatRouter.HandleFunc("/import", h.ImportChats).Methods("POST")

	chatRouter.HandleFunc("/{chatId}/chat-message/{messageId}/retry", h.RetryChatMessage).Methods("POST")
	chatRouter.HandleFunc("/{chatId}/chat-message/{messageId}/active-job", h.GetActiveChatMessageJob).Methods("GET")
	chatRouter.HandleFunc("/{chatId}/chat-message", h.GetChatMessages).Methods("GET")
	chatRouter.HandleFunc("/{chatId}/chat-message", h.CreateChatMessage).Methods("POST")
	chatRouter.HandleFunc("/{chatId}/welcome-message", h.CreateWelcomeMessage).Methods("POST")
	chatRouter.HandleFunc("/{chatId}/available-rituals", h.GetAvailableRituals).Methods("GET")
	chatRouter.HandleFunc("/{chatId}/file-attachment", h.CreateFileAttachment).Methods("POST")
	chatRouter.HandleFunc("/chat-message/{id}", h.GetChatMessage).Methods("GET")
	chatRouter.HandleFunc("/{id}/context", h.GetChatContext).Methods("GET")
	chatRouter.HandleFunc("/{id}/context", h.PatchChatContext).Methods("PATCH")

	chatMCPRouter := chatRouter.PathPrefix("/{chatId}").Subrouter()
	chatMCPRouter.HandleFunc("/mcp-servers", h.ListChatMCPServers).Methods("GET")
	chatMCPRouter.HandleFunc("/available-mcp-servers", h.ListAvailableChatMCPServers).Methods("GET")
	chatMCPRouter.HandleFunc("/mcp-servers/{mcpServerId}", h.AddMCPServerToChat).Methods("POST")
	chatMCPRouter.HandleFunc("/mcp-servers/{mcpServerId}", h.RemoveMCPServerFromChat).Methods("DELETE")
	// Backward-compatible singular aliases.
	chatMCPRouter.HandleFunc("/mcp-server", h.ListChatMCPServers).Methods("GET")
	chatMCPRouter.HandleFunc("/available-mcp-server", h.ListAvailableChatMCPServers).Methods("GET")
	chatMCPRouter.HandleFunc("/mcp-server/{mcpServerId}", h.AddMCPServerToChat).Methods("POST")
	chatMCPRouter.HandleFunc("/mcp-server/{mcpServerId}", h.RemoveMCPServerFromChat).Methods("DELETE")

	// Chat CRUD routes - these use path variables so they come after more specific routes
	chatRouter.HandleFunc("", h.ListChats).Methods("GET")
	chatRouter.HandleFunc("", h.CreateChat).Methods("POST")
	chatRouter.HandleFunc("/{id}", h.GetChat).Methods("GET")
	chatRouter.HandleFunc("/{id}", h.UpdateChat).Methods("PUT")
	chatRouter.HandleFunc("/{id}", h.PatchChat).Methods("PATCH")
	chatRouter.HandleFunc("/{id}/mark-read", h.MarkChatRead).Methods("POST")
	chatRouter.HandleFunc("/{id}", h.DeleteChat).Methods("DELETE")
	chatRouter.HandleFunc("/{id}/export", h.ExportChat).Methods("GET")
}
