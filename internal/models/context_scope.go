package models

import "strings"

// ContextScope is how much of the account a chat may read. It is stored as a string, not a
// boolean, so later scopes (a project, an allow-list of sources) can be added without changing
// the column, the API or the export formats. Every rule asks Chat.IsSandboxed (or a helper built
// on it), never the value directly, so a new scope changes one accessor.
type ContextScope string

const (
	// ContextScopeAccount is the default: the chat reads the whole account, as any chat does.
	ContextScopeAccount ContextScope = "account"
	// ContextScopeSandbox: the chat reads only itself. See Chat.IsSandboxed for what that means.
	ContextScopeSandbox ContextScope = "sandbox"
)

// ParseContextScope reads a scope from a request or a stored value. ok is false for anything
// but the known scopes; an empty value is not a scope (callers decide what absent means).
func ParseContextScope(raw string) (ContextScope, bool) {
	switch s := ContextScope(strings.ToLower(strings.TrimSpace(raw))); s {
	case ContextScopeAccount, ContextScopeSandbox:
		return s, true
	}
	return "", false
}

// Valid reports whether s is a known scope.
func (s ContextScope) Valid() bool {
	_, ok := ParseContextScope(string(s))
	return ok
}

// OrDefault reads an empty scope (an older row or export, a request that left it out) as account.
func (s ContextScope) OrDefault() ContextScope {
	if s == "" {
		return ContextScopeAccount
	}
	return s
}
