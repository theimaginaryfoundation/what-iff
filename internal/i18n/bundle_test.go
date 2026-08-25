package i18n_test

import (
	"testing"

	i18n "github.com/theimaginaryfoundation/what-iff/internal/i18n"
)

func TestT_KnownKey(t *testing.T) {
	got := i18n.T("chat.not_found")
	want := "chat not found"
	if got != want {
		t.Errorf("T(%q) = %q; want %q", "chat.not_found", got, want)
	}
}

func TestT_UnknownKeyFallsBackToID(t *testing.T) {
	id := "no.such.message.id"
	got := i18n.T(id)
	if got != id {
		t.Errorf("T(%q) = %q; want fallback to ID %q", id, got, id)
	}
}

func TestT_AllErrorKeys(t *testing.T) {
	// Plain keys (no template variables) from en-internal.toml.
	keys := []struct {
		id   string
		want string
	}{
		{"chat.not_found", "chat not found"},
		{"tx.start_failed", "failed to start transaction"},
		{"tx.rollback_failed", "failed to rollback transaction"},
		{"tx.commit_failed", "failed to commit transaction"},
	}
	for _, tc := range keys {
		t.Run(tc.id, func(t *testing.T) {
			got := i18n.T(tc.id)
			if got != tc.want {
				t.Errorf("T(%q) = %q; want %q", tc.id, got, tc.want)
			}
		})
	}
}

func TestT1_SharedCRUDKeys(t *testing.T) {
	// Shared parameterized CRUD keys from en-internal.toml.
	cases := []struct {
		id     string
		entity string
		want   string
	}{
		{"query.failed", "chat", "failed to query chat"},
		{"query.failed", "memory", "failed to query memory"},
		{"query.failed", "personality", "failed to query personality"},
		{"query.failed", "ritual", "failed to query ritual"},
		{"query.failed", "job", "failed to query job"},
		{"query.failed", "file attachment", "failed to query file attachment"},
		{"query.failed", "user", "failed to query user"},
		{"create.failed", "chat", "failed to create chat"},
		{"update.failed", "role", "failed to update role"},
		{"delete.failed", "memory", "failed to delete memory"},
		{"count.failed", "jobs", "failed to count jobs"},
		{"list.failed", "rituals", "failed to list rituals"},
		{"reload.failed", "role", "failed to reload role"},
	}
	for _, tc := range cases {
		name := tc.id + "/" + tc.entity
		t.Run(name, func(t *testing.T) {
			got := i18n.T1(tc.id, "Entity", tc.entity)
			if got != tc.want {
				t.Errorf("T1(%q, Entity=%q) = %q; want %q", tc.id, tc.entity, got, tc.want)
			}
		})
	}
}

func TestTWith_TemplateKeys(t *testing.T) {
	// Template keys from en-internal.toml — verify variable substitution.
	cases := []struct {
		id   string
		data interface{}
		want string
	}{
		{
			"chat.not_found_or_unauthorized",
			map[string]interface{}{"ChatID": "c1", "UserID": "u1"},
			"chat not found or user not authorized (chat_id=c1, user_id=u1)",
		},
		{
			"memory.not_found_or_unauthorized",
			map[string]interface{}{"MemoryID": "m1", "UserID": "u1"},
			"memory not found or user not authorized (memory_id=m1, user_id=u1)",
		},
		{
			"job.not_found_or_unauthorized",
			map[string]interface{}{"JobID": "j1", "UserID": "u1"},
			"job not found or user not authorized (job_id=j1, user_id=u1)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got := i18n.TWith(tc.id, tc.data)
			if got != tc.want {
				t.Errorf("TWith(%q) = %q; want %q", tc.id, got, tc.want)
			}
		})
	}
}
