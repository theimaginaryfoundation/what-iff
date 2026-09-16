package provider

import "context"

// OpenAIKeyResolver returns the credential for the actor ctx belongs to, or ""
// when that actor has none.
//
// Named for OpenAI because that is where per-account credentials started; it
// now serves every provider, and every CredentialRule carries one.
//
// Keys belong to accounts, not to the process: a self-hosted instance with two
// users has two people's credentials and two people's bills. Resolving per
// request is what makes that true, and it is cheap here because the SDK client
// value is copied into six independent holders (OpenAIProvider, the recall and
// memory tools, the file-chunk pipeline, the memory handler, and the plugin
// embedder) that all share one HTTP client. Resolving at that shared transport
// reaches all six without touching a single call site.
//
// The actor survives into background work: every detach point in the agent
// goes through middleware.CopyUserToIDContext, so an agent job or a scheduled
// run still resolves to the user who owns it.
type OpenAIKeyResolver func(ctx context.Context) string
