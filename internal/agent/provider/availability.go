package provider

import "errors"

// ErrProviderUnavailable is returned when a provider is asked to do work
// without a usable client behind it.
//
// Provider clients are constructed unconditionally, so in a running server this
// cannot happen — the agent is built by NewAgent, which builds all of them. It
// can happen in a test, or in any future code that assembles an Agent by hand,
// and every provider method dereferences its client. Returning an error there
// rather than panicking keeps a misassembled provider from taking down the job
// worker that called it, which is the same reasoning that puts the panic
// recovery for push notifications in the core rather than in implementations.
var ErrProviderUnavailable = errors.New("provider has no client configured")
