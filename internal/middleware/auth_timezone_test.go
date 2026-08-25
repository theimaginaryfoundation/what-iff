package middleware

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestCopyUserToIDContext_CopiesClientTimezone(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	src := context.WithValue(context.Background(), UserIDKey, userID)
	src = context.WithValue(src, ClientTimezoneKey, "America/Los_Angeles")

	dst, ok := CopyUserToIDContext(src, context.Background())
	if !ok {
		t.Fatalf("expected ok=true")
	}

	if gotID, ok := GetUserIDFromContext(dst); !ok || gotID != userID {
		t.Fatalf("expected user_id to be copied")
	}

	if tz, ok := GetClientTimezoneFromContext(dst); !ok || tz != "America/Los_Angeles" {
		t.Fatalf("expected client timezone to be copied, got %q (ok=%v)", tz, ok)
	}
}
