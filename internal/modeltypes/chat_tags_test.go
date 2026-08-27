package modeltypes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAndValidateChatTags(t *testing.T) {
	tests := []struct {
		name      string
		tags      []string
		want      []string
		wantErr   string
		expectNil bool
	}{
		{
			name:      "nil input returns nil output",
			tags:      nil,
			expectNil: true,
		},
		{
			name: "empty-but-non-nil input returns empty slice",
			tags: []string{},
			want: []string{},
		},
		{
			name: "trims each tag",
			tags: []string{"  work  ", "\tpersonal\n"},
			want: []string{"work", "personal"},
		},
		{
			name:    "too many tags",
			tags:    make([]string, MaxChatTags+1),
			wantErr: "chat can have at most 10 tags",
		},
		{
			name:    "tag empty after trimming",
			tags:    []string{"ok", "   "},
			wantErr: "chat tags cannot be empty",
		},
		{
			name:    "tag too long after trimming",
			tags:    []string{strings.Repeat("a", MaxChatTagLength+1)},
			wantErr: "chat tags must be at most 10 characters",
		},
		{
			name: "tag exactly at the length limit is valid",
			tags: []string{strings.Repeat("a", MaxChatTagLength)},
			want: []string{strings.Repeat("a", MaxChatTagLength)},
		},
		{
			name: "tag count exactly at the limit is valid",
			tags: func() []string {
				tags := make([]string, MaxChatTags)
				for i := range tags {
					tags[i] = "tag"
				}
				return tags
			}(),
			want: func() []string {
				tags := make([]string, MaxChatTags)
				for i := range tags {
					tags[i] = "tag"
				}
				return tags
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeAndValidateChatTags(tt.tags)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tt.expectNil {
				assert.Nil(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateChatTags(t *testing.T) {
	tests := []struct {
		name    string
		tags    []string
		wantErr bool
	}{
		{name: "valid tags", tags: []string{"work", "personal"}, wantErr: false},
		{name: "invalid: empty tag", tags: []string{""}, wantErr: true},
		{name: "invalid: too many tags", tags: make([]string, MaxChatTags+1), wantErr: true},
		{name: "nil tags is valid (no-op)", tags: nil, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateChatTags(tt.tags)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
