package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Words the job tools accept for thread_id besides a conversation id.
const (
	jobThreadCurrent = "current" // the conversation the tool is called from
	jobThreadNew     = "new"     // a dedicated thread, made on the job's next run
)

// jobThreadChats is what resolving a job's thread reads; *datastore.Datastore satisfies it.
type jobThreadChats interface {
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
}

// errJobThreadLookup is a datastore failure while checking a thread, as opposed to a thread the
// model named wrongly; the tools report it as a retryable failure without the underlying error.
var errJobThreadLookup = errors.New("could not check that conversation")

// resolveJobThread turns a thread_id the model passed into where the job runs. "current" is the
// calling conversation; "new" or "" is a dedicated thread (target nil: the scheduler makes one on
// the next run, as for a job created without a thread). Any other value must be the id of one of
// the user's conversations that is not sandboxed: a job may attach the owner's skills and their MCP
// servers, which a sandbox keeps out. A conversation that is deleted later is not an error here: its
// jobs lose the link and get a dedicated thread on their next run.
//
// A bad value comes back as an error whose text is meant for the model.
func resolveJobThread(ctx context.Context, chats jobThreadChats, caller *models.Chat, raw string) (*uuid.UUID, error) {
	value := strings.TrimSpace(raw)
	switch strings.ToLower(value) {
	case "", jobThreadNew:
		return nil, nil
	case jobThreadCurrent:
		id := caller.ID
		return &id, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("thread_id must be a conversation id from list (kind=\"conversations\"), %q or %q", jobThreadCurrent, jobThreadNew)
	}
	if id == caller.ID {
		return &id, nil
	}
	target, err := chats.GetChat(ctx, caller.UserID, id)
	if errors.Is(err, datastore.ErrChatNotFound) {
		return nil, fmt.Errorf("no conversation with id %s (it may have been deleted); use list (kind=\"conversations\") to find one", id)
	}
	if err != nil {
		return nil, errJobThreadLookup
	}
	if target.IsSandboxed() {
		return nil, fmt.Errorf("conversation %s is sandboxed, so jobs cannot run there", id)
	}
	return &id, nil
}
