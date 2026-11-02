package database

import (
	"context"
	"strings"
	"testing"

	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/dialect/sql/schema"

	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
)

func TestConcurrentIndexDDL(t *testing.T) {
	a, b := &schema.Column{Name: "user_memories"}, &schema.Column{Name: "status"}
	live := map[string]bool{"user_memories": true, "status": true}

	tests := []struct {
		name string
		idx  *schema.Index
		want string
	}{
		{"plain", &schema.Index{Name: "memory_owner_status", Columns: []*schema.Column{a, b}},
			`CREATE INDEX CONCURRENTLY IF NOT EXISTS "memory_owner_status" ON "memories" ("user_memories", "status")`},
		{"unique", &schema.Index{Name: "memory_owner_u", Unique: true, Columns: []*schema.Column{a}},
			`CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "memory_owner_u" ON "memories" ("user_memories")`},
		{"annotated is left to the migration", &schema.Index{Name: "memory_partial", Columns: []*schema.Column{a}, Annotation: &entsql.IndexAnnotation{Where: "status = 'active'"}}, ""},
		{"column the migration adds", &schema.Index{Name: "memory_new_col", Columns: []*schema.Column{{Name: "not_yet"}}}, ""},
		{"name over the identifier limit", &schema.Index{Name: strings.Repeat("x", postgresMaxIdentifier+1), Columns: []*schema.Column{a}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := concurrentIndexDDL("memories", tt.idx, live)
			if ok != (tt.want != "") || got != tt.want {
				t.Errorf("concurrentIndexDDL() = %q, %v; want %q", got, ok, tt.want)
			}
		})
	}
}

// The memory list indexes must lead with the owner column, or they can't seek one user's rows.
func TestMemoryListIndexesLeadWithOwner(t *testing.T) {
	want := map[string][]string{
		"memory_user_memories_status_created_at": {"user_memories", "status", "created_at"},
		"memory_user_memories_status_updated_at": {"user_memories", "status", "updated_at"},
		"memory_chat_memories":                   {"chat_memories"},
	}
	for _, idx := range migrate.MemoriesTable.Indexes {
		cols, ok := want[idx.Name]
		if !ok {
			continue
		}
		delete(want, idx.Name)
		got := make([]string, len(idx.Columns))
		for i, c := range idx.Columns {
			got[i] = c.Name
		}
		if strings.Join(got, ",") != strings.Join(cols, ",") {
			t.Errorf("%s columns = %v, want %v", idx.Name, got, cols)
		}
	}
	for name := range want {
		t.Errorf("index %s is missing from the memories schema", name)
	}
}

func TestPrebuildIndexesSkipsOtherDatabases(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite3")
	n, err := PrebuildIndexes(context.Background(), nil, nil)
	if err != nil || n != 0 {
		t.Fatalf("PrebuildIndexes() = %d, %v; want 0, nil", n, err)
	}
}
