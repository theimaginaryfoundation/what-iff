package database

import "testing"

func TestDatabaseTypeDefaultsToPostgres(t *testing.T) {
	t.Setenv("DB_TYPE", "")
	if got := databaseType(); got != "postgres" {
		t.Fatalf("databaseType() = %q, want postgres", got)
	}
}

func TestDatabaseTypeUsesConfiguredValue(t *testing.T) {
	t.Setenv("DB_TYPE", "mysql")
	if got := databaseType(); got != "mysql" {
		t.Fatalf("databaseType() = %q, want mysql", got)
	}
}

func TestDatabaseTypeTrimsEmptyConfiguration(t *testing.T) {
	t.Setenv("DB_TYPE", "   ")
	if got := databaseType(); got != "postgres" {
		t.Fatalf("databaseType() = %q, want postgres", got)
	}
}
