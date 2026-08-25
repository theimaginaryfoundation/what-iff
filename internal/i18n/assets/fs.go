// Package assets holds the default English translation bundles for the application.
// en.toml contains customer-facing strings; en-internal.toml contains internal
// logging strings. Both are embedded at compile time so the binary is self-contained.
package assets

import _ "embed"

// EnData is the raw content of en.toml (customer-facing strings), embedded at compile time.
//
//go:embed en.toml
var EnData []byte

// EnInternalData is the raw content of en-internal.toml (internal logging strings), embedded at compile time.
//
//go:embed en-internal.toml
var EnInternalData []byte
