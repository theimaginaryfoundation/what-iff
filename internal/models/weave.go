package models

import (
	"time"

	"github.com/google/uuid"
)

// Weave is a named set of sandboxed threads that may read each other: each member's sandbox
// widens from "this thread" to "this thread and its weave peers" for reads, while every write
// stays in the thread that makes it. See docs/adr/0x030-thread-weaves.md.
type Weave struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Chats are the member threads, oldest first.
	Chats     []ChatRef `json:"chats"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
