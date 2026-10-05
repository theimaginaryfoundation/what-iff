package exporter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/stcard"
)

func TestWriteSillyTavernCardsWritesOneCardPerPersonality(t *testing.T) {
	native := PersonalityInput{ID: uuid.New(), Name: "Native", SystemPrompt: "You are native."}
	imported := PersonalityInput{
		ID:           uuid.New(),
		Name:         "Imported",
		SystemPrompt: stcard.Compose(stcard.Segments{SystemPrompt: "Sys.", Description: "Desc."}),
		CharacterCard: map[string]any{
			"creator":   "card-author",
			"first_mes": "Hello!",
		},
	}
	tree := NewMemTree()

	if err := WriteSillyTavernCards(tree, []PersonalityInput{native, imported}, "gori"); err != nil {
		t.Fatal(err)
	}

	read := func(p PersonalityInput) map[string]any {
		t.Helper()
		b, ok := tree.Get("personalities/" + p.ID.String() + "/sillytavern.json")
		if !ok {
			t.Fatalf("no card written for %s", p.Name)
		}
		var card struct {
			Spec string         `json:"spec"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(b, &card); err != nil {
			t.Fatal(err)
		}
		if card.Spec != "chara_card_v2" {
			t.Errorf("%s spec = %q", p.Name, card.Spec)
		}
		return card.Data
	}

	if got := read(native); got["creator"] != "gori" || got["system_prompt"] != "You are native." {
		t.Errorf("native card = %v", got)
	}
	got := read(imported)
	if got["creator"] != "card-author" || got["first_mes"] != "Hello!" {
		t.Errorf("imported card lost its passthrough fields: %v", got)
	}
	if got["system_prompt"] != "Sys." || got["description"] != "Desc." {
		t.Errorf("imported card prompt not split back into segments: %v", got)
	}
}

func TestWritePersonalitiesCarriesTheCharacterCardForAccountRoundTrip(t *testing.T) {
	id := uuid.New()
	tree := NewMemTree()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	err := WritePersonalities(tree, []PersonalityInput{{
		ID: id, Name: "Ada", SystemPrompt: "sp", CreatedAt: at, UpdatedAt: at,
		CharacterCard: map[string]any{"tags": []any{"fox"}},
	}, {
		ID: uuid.New(), Name: "Native", SystemPrompt: "sp", CreatedAt: at, UpdatedAt: at,
	}})
	if err != nil {
		t.Fatal(err)
	}

	b, _ := tree.Get("personalities/" + id.String() + "/personality.json")
	var pf PersonalityFile
	if err := json.Unmarshal(b, &pf); err != nil {
		t.Fatal(err)
	}
	if tags, _ := pf.CharacterCard["tags"].([]any); len(tags) != 1 || tags[0] != "fox" {
		t.Errorf("CharacterCard = %v", pf.CharacterCard)
	}

	for _, p := range tree.Paths() {
		if strings.HasSuffix(p, "/personality.json") && p != "personalities/"+id.String()+"/personality.json" {
			raw, _ := tree.Get(p)
			if strings.Contains(string(raw), "character_card") {
				t.Errorf("native personality.json should omit character_card: %s", raw)
			}
		}
	}
}
