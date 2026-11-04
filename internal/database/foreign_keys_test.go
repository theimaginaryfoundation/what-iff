package database

import (
	"testing"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
)

// Every row that points at an MCP server goes with it. A foreign key without a cascade refuses the
// delete instead: OAuth sessions once made servers that had completed OAuth impossible to delete.
func TestMCPServerReferencesCascade(t *testing.T) {
	found := 0
	for _, table := range migrate.Tables {
		for _, fk := range table.ForeignKeys {
			if fk.RefTable == nil || fk.RefTable.Name != migrate.McpServersTable.Name {
				continue
			}
			found++
			if fk.OnDelete != schema.Cascade {
				t.Errorf("%s.%s references mcp_servers with ON DELETE %s, want CASCADE", table.Name, fk.Symbol, fk.OnDelete)
			}
		}
	}
	if found == 0 {
		t.Fatal("no foreign keys reference mcp_servers; the test is not looking at the schema")
	}
}
