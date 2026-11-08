package filechange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestNotifyReachesSubscribersInOrderAndSkipsNilFiles(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var got []string
	Subscribe(func(_ context.Context, ev Event) { got = append(got, "a:"+string(ev.Kind)) })
	Subscribe(func(_ context.Context, ev Event) { got = append(got, "b:"+ev.File.Name) })
	Subscribe(nil)

	Notify(context.Background(), Event{Kind: KindContent}) // no file: dropped
	Notify(context.Background(), Event{Kind: KindRename, UserID: uuid.New(), File: &models.FileAttachment{Name: "n.md"}, OldName: "o.md"})
	require.Equal(t, []string{"a:rename", "b:n.md"}, got)
}

func TestNotifySurvivesAPanickingSubscriber(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	reached := false
	Subscribe(func(context.Context, Event) { panic("boom") })
	Subscribe(func(context.Context, Event) { reached = true })

	require.NotPanics(t, func() {
		Notify(context.Background(), Event{Kind: KindContent, UserID: uuid.New(), File: &models.FileAttachment{Name: "n.md"}})
	})
	require.True(t, reached)
}
