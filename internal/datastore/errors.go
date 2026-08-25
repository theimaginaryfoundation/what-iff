package datastore

import "errors"

// Common errors returned by datastore operations
var (
	// General errors
	ErrUnauthorized      = errors.New("user not authorized for this operation")
	ErrInternalDatastore = errors.New("internal datastore error")

	// ErrCannotModifySuperAdmin guards mutations that must never touch a
	// super_admin user (e.g. toggling experimental models). Returned by public
	// datastore operations and by the overlay's admin user management.
	ErrCannotModifySuperAdmin = errors.New("cannot modify super_admin users")

	// Content-related errors
	ErrContentIdeaNotFound  = errors.New("content idea not found")
	ErrContentBriefNotFound = errors.New("content brief not found")
	ErrContentDraftNotFound = errors.New("content draft not found")

	// Project-related errors
	ErrProjectNotFound = errors.New("project not found")

	// Platform-related errors
	ErrPlatformNotFound      = errors.New("platform not found")
	ErrPlatformAlreadyExists = errors.New("platform with this name already exists")

	// Interview question errors
	ErrInterviewQuestionNotFound = errors.New("interview question not found")

	// Job-related errors
	ErrJobNotFound      = errors.New("job not found")
	ErrInvalidJobStatus = errors.New("invalid job status")

	// AgentJob-related errors
	ErrAgentJobNotFound     = errors.New("agent job not found")
	ErrInvalidAgentJob      = errors.New("invalid agent job")
	ErrInvalidAgentSchedule = errors.New("invalid agent job schedule")

	// Chat-related errors
	ErrChatNotFound                            = errors.New("chat not found")
	ErrChatMessageNotFound                     = errors.New("chat message not found")
	ErrGenerationExpressionPersonalityMismatch = errors.New("generation expression does not belong to this chat's personality")
	ErrInvalidMessageOriginFilter              = errors.New("invalid message origin filter")
	ErrFavoriteLimitExceeded                   = errors.New("favorite chat best-effort limit exceeded")

	// Memory-related errors
	ErrMemoryNotFound = errors.New("memory not found")

	// Personality-related errors
	ErrPersonalityNotFound = errors.New("personality not found")

	// ErrPersonalityExpressionNotDeletable is returned when deleting a reserved expression key (e.g. thinking).
	ErrPersonalityExpressionNotDeletable = errors.New("personality expression cannot be deleted")

	// File attachment-related errors
	ErrFileAttachmentNotFound = errors.New("file attachment not found")
	ErrInvalidRequestBody     = errors.New("invalid request body")

	// Ritual-related errors
	ErrRitualNotFound = errors.New("ritual not found")

	// MCP server-related errors
	ErrMCPServerNotFound = errors.New("mcp server not found")

	// Webhook token-related errors
	ErrWebhookTokenNotFound = errors.New("webhook token not found")
	ErrWebhookTokenInvalid  = errors.New("webhook token is invalid")

	// Personality generation flow errors
	ErrFlowNotFound                   = errors.New("personality gen flow not found")
	ErrFlowGenerationJobAlreadyActive = errors.New("personality generation job already active for flow")

	// Safety violation event-related errors
	ErrSafetyViolationEventNotFound = errors.New("safety violation event not found")

	// Mood-related errors
	ErrMoodNotFound = errors.New("mood not found")

	// Billing-related errors
	ErrBillingDetailsNotFound = errors.New("billing details not found")

	// Account backup-related errors
	ErrUnsupportedAccountBackupVersion  = errors.New("unsupported account backup format version")
	ErrAccountBackupDefaultModelMissing = errors.New("target user has no default model for account backup import")
)
