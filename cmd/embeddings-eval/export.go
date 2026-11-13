package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/exporter"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// memoryScope mirrors the two memory scopes an account export carries (Summary-scope rows are
// not exported, and GetRelatedMemories never returns them anyway).
type memoryScope int

const (
	scopeUser memoryScope = iota // user.json and personality-<id>.json
	scopeChat                    // chat.json
)

// evalMemory is one exported memory with the fields retrieval visibility depends on.
type evalMemory struct {
	ID                  uuid.UUID
	Content             string
	CreatedAt           time.Time
	Scope               memoryScope
	ChatID              uuid.UUID // Chat scope only
	PinnedPersonalityID uuid.UUID // User scope pinned to a personality; uuid.Nil when unpinned
}

// evalTurn is one user message from conversations.json: what the memory query is built from.
type evalTurn struct {
	ChatID        uuid.UUID
	ChatName      string
	PersonalityID uuid.UUID
	Text          string
	CreatedAt     time.Time
}

type evalExport struct {
	Memories []evalMemory
	Turns    []evalTurn
}

// conversation is the subset of exporter's conversations.json record this tool reads.
type conversation struct {
	UUID                 string     `json:"uuid"`
	Name                 string     `json:"name"`
	WhatiffPersonalityID *uuid.UUID `json:"whatiff_personality_id"`
	ChatMessages         []struct {
		Sender    string    `json:"sender"`
		Text      string    `json:"text"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"chat_messages"`
}

// loadExport reads an account-export ZIP (the one POST /account/export emails): conversations.json
// for the user turns and the nested memories.zip for the memory corpus.
func loadExport(zipPath string) (*evalExport, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("opening export: %w", err)
	}
	defer zr.Close()

	out := &evalExport{}
	var sawConversations, sawMemories bool
	for _, f := range zr.File {
		switch f.Name {
		case exporter.ConversationsFile:
			sawConversations = true
			if err := readZipFile(f, func(r io.Reader) error { return decodeConversations(r, out) }); err != nil {
				return nil, fmt.Errorf("reading %s: %w", f.Name, err)
			}
		case "memories.zip":
			sawMemories = true
			if err := readZipFile(f, func(r io.Reader) error { return decodeMemoriesZip(r, out) }); err != nil {
				return nil, fmt.Errorf("reading %s: %w", f.Name, err)
			}
		}
	}
	if !sawConversations || !sawMemories {
		return nil, fmt.Errorf("export is missing conversations.json or memories.zip")
	}
	return out, nil
}

func readZipFile(f *zip.File, fn func(io.Reader) error) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return fn(rc)
}

// decodeConversations streams the conversations.json array one chat at a time (it can be tens of
// MB) and keeps only the human turns.
func decodeConversations(r io.Reader, out *evalExport) error {
	dec := json.NewDecoder(bufio.NewReader(r))
	if _, err := dec.Token(); err != nil { // opening [
		return err
	}
	for dec.More() {
		var c conversation
		if err := dec.Decode(&c); err != nil {
			return err
		}
		chatID, err := uuid.Parse(c.UUID)
		if err != nil {
			continue // a conversation without a source id cannot own chat-scoped memories
		}
		var personalityID uuid.UUID
		if c.WhatiffPersonalityID != nil {
			personalityID = *c.WhatiffPersonalityID
		}
		for _, m := range c.ChatMessages {
			if m.Sender != "human" || strings.TrimSpace(m.Text) == "" {
				continue
			}
			out.Turns = append(out.Turns, evalTurn{
				ChatID:        chatID,
				ChatName:      c.Name,
				PersonalityID: personalityID,
				Text:          m.Text,
				CreatedAt:     m.CreatedAt,
			})
		}
	}
	return nil
}

// decodeMemoriesZip reads the nested memory ZIP: chat.json, user.json and one
// personality-<id>.json per personality with pinned memories, each JSON Lines of MemoryRecord.
func decodeMemoriesZip(r io.Reader, out *evalExport) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		name := path.Base(f.Name)
		var scope memoryScope
		var pinned uuid.UUID
		switch {
		case name == "chat.json":
			scope = scopeChat
		case name == "user.json":
			scope = scopeUser
		case strings.HasPrefix(name, "personality-") && strings.HasSuffix(name, ".json"):
			scope = scopeUser
			pinned, err = uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(name, "personality-"), ".json"))
			if err != nil {
				return fmt.Errorf("%s: bad personality id: %w", name, err)
			}
		default:
			continue
		}
		if err := readZipFile(f, func(r io.Reader) error {
			dec := json.NewDecoder(r)
			for dec.More() {
				var rec models.MemoryRecord
				if err := dec.Decode(&rec); err != nil {
					return err
				}
				m := evalMemory{ID: rec.ID, Content: rec.Content, CreatedAt: rec.CreatedAt, Scope: scope, PinnedPersonalityID: pinned}
				if scope == scopeChat {
					if rec.ChatID == nil {
						continue
					}
					m.ChatID = *rec.ChatID
				}
				out.Memories = append(out.Memories, m)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}
