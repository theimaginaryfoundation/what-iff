package datastore

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestListAccountActivityRejectsUninitializedDatastore(t *testing.T) {
	_, err := (*Datastore)(nil).ListAccountActivity(t.Context(), uuid.New(), 25)
	if !errors.Is(err, errAccountActivityDatastoreUnavailable) {
		t.Fatalf("ListAccountActivity error = %v, want %v", err, errAccountActivityDatastoreUnavailable)
	}
}

func TestAccountActivityCategoriesIncludeAccountImport(t *testing.T) {
	for _, category := range accountActivityCategories {
		if category == auditCategoryAccountImport {
			return
		}
	}
	t.Fatalf("account activity categories = %v, want %q", accountActivityCategories, auditCategoryAccountImport)
}

func TestAccountActivityMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		message      string
		wantMessage  string
		wantMetadata map[string]any
	}{
		{
			name:         "separates serialized metadata",
			message:      `account import complete | metadata={"success":true,"imported":2}`,
			wantMessage:  "account import complete",
			wantMetadata: map[string]any{"success": true, "imported": float64(2)},
		},
		{
			name:        "leaves entries without metadata unchanged",
			message:     "account export requested",
			wantMessage: "account export requested",
		},
		{
			name:        "leaves invalid metadata visible",
			message:     "account import failed | metadata=not-json",
			wantMessage: "account import failed | metadata=not-json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMessage, gotMetadata := accountActivityMessage(tt.message)
			if gotMessage != tt.wantMessage {
				t.Errorf("message = %q, want %q", gotMessage, tt.wantMessage)
			}
			if !reflect.DeepEqual(gotMetadata, tt.wantMetadata) {
				t.Errorf("metadata = %#v, want %#v", gotMetadata, tt.wantMetadata)
			}
		})
	}
}
