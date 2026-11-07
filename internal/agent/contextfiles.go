package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// The files in a conversation's context (ADR 0x043 §4): one rule, used by the Files tab through
// GET /chat/{chatId}/context-files and by the private build to hydrate the conversation's
// container, so the two can never drift.

// ContextReason says why a file is in the conversation's context. A file can have several.
type ContextReason string

const (
	// ContextReasonAttachedRecent: on one of the last ten user messages.
	ContextReasonAttachedRecent ContextReason = "attached_recent"
	// ContextReasonAttachedSinceCheckpoint: on any message since the last checkpoint.
	ContextReasonAttachedSinceCheckpoint ContextReason = "attached_since_checkpoint"
	// ContextReasonRecalled: a find_context result since the last checkpoint.
	ContextReasonRecalled ContextReason = "recalled"
	// ContextReasonProducedThisTurn: written by a tool in the turn in progress (private build).
	ContextReasonProducedThisTurn ContextReason = "produced_this_turn"
	// ContextReasonAgentWritten: written by the agent's file tools in this conversation.
	ContextReasonAgentWritten ContextReason = "agent_written"
	// ContextReasonSkillBundle: a file of a skill bundle active in the conversation.
	ContextReasonSkillBundle ContextReason = "skill_bundle"
)

// ContextFile is one file in the conversation's context.
type ContextFile struct {
	Attachment *models.FileAttachment `json:"file"`
	// WorkspacePath is where a container mounts it, relative to /workspace: a message attachment or
	// agent file by its name alone (folders are gallery labels), a bundle file under skills/<slug>/.
	WorkspacePath string          `json:"workspace_path"`
	Reasons       []ContextReason `json:"reasons"`
	// ShadowedBy names the newer file that takes the same workspace path, so this one is not
	// mounted. The Files tab shows it greyed out; the container never sees it.
	ShadowedBy *uuid.UUID `json:"shadowed_by,omitempty"`
}

// ContainerStatus is what the private build knows about the conversation's container, if any:
// its state and, per workspace path, whether the container's copy is current. Nil in public.
type ContainerStatus struct {
	State string            `json:"state"`
	Files map[string]string `json:"files,omitempty"`
}

// Seams the private build sets in an init() (like skillScriptsRunnableForChat):
//   - extraContextFilesForChat adds the files tools produced in the turn in progress.
//   - containerStatusForChat reports the conversation's container for the endpoint.
var (
	extraContextFilesForChat func(ctx context.Context, chat *models.Chat) []*models.FileAttachment
	containerStatusForChat   func(ctx context.Context, chat *models.Chat, files []ContextFile) *ContainerStatus
)

// ContainerStatusForChat is what the private build reports for the conversation's container, or
// nil when there is no container integration.
func (a *Agent) ContainerStatusForChat(ctx context.Context, chat *models.Chat, files []ContextFile) *ContainerStatus {
	if containerStatusForChat == nil {
		return nil
	}
	return containerStatusForChat(ctx, chat, files)
}

const recentUserMessagesInContext = 10

// ContextFilesForChat lists the files an agent working in chat is expected to see, newest first:
// the attachments on every message since the last checkpoint and the files find_context pulled in
// for them, the attachments on the last ten user messages, the files tools produced this turn, the
// agent's own files written in this conversation, and the active skills' bundle files. Files that
// share a workspace path are all listed; the newest takes the path and the others say which one.
func (a *Agent) ContextFilesForChat(ctx context.Context, chat *models.Chat) ([]ContextFile, error) {
	if a == nil || chat == nil {
		return nil, nil
	}
	c := newContextFileCollector()
	if a.ds != nil {
		sinceCheckpoint, err := a.listChatMessagesForContext(ctx, chat, chat.LastCheckpointAt)
		if err != nil {
			return nil, err
		}
		for _, msg := range sinceCheckpoint {
			for _, att := range msg.Attachments {
				c.add(att, "", ContextReasonAttachedSinceCheckpoint)
			}
			for _, att := range a.recalledAttachments(ctx, chat, msg) {
				c.add(att, "", ContextReasonRecalled)
			}
		}
		recent, err := a.listChatMessagesForContext(ctx, chat, nil)
		if err != nil {
			return nil, err
		}
		userSeen := 0
		for _, msg := range recent {
			if msg == nil || msg.Origin != models.MessageOriginUser {
				continue
			}
			for _, att := range msg.Attachments {
				c.add(att, "", ContextReasonAttachedRecent)
			}
			if userSeen++; userSeen >= recentUserMessagesInContext {
				break
			}
		}
		agentFiles, err := a.ds.ListAgentFilesCreatedInChat(ctx, chat.UserID, chat.ID)
		if err != nil {
			return nil, err
		}
		for _, att := range agentFiles {
			c.add(att, "", ContextReasonAgentWritten)
		}
		for _, skill := range a.activeSkillsForContextFiles(ctx, chat) {
			files, err := a.ds.ListSkillFiles(ctx, chat.UserID, skill.ID)
			if err != nil {
				a.logger.Warn("context files: failed listing a skill's files", zap.String("skill_id", skill.ID.String()), zap.Error(err))
				continue
			}
			for _, att := range files {
				p, err := SkillBundleWorkspacePath(skill.Slug, datastore.SkillFileRelPath(att, skill.Slug))
				if err != nil {
					continue
				}
				c.add(att, p, ContextReasonSkillBundle)
			}
		}
	}
	if extraContextFilesForChat != nil {
		for _, att := range extraContextFilesForChat(ctx, chat) {
			c.add(att, "", ContextReasonProducedThisTurn)
		}
	}
	return c.result(), nil
}

// activeSkillsForContextFiles is the turn's active skills inside a turn, else the skills loaded in
// the chat; a sandboxed conversation gets none (prepareTurnSkills applies the same rule in a turn).
func (a *Agent) activeSkillsForContextFiles(ctx context.Context, chat *models.Chat) []*models.Ritual {
	if chatCtx := turnChatContextFrom(ctx); chatCtx != nil {
		return chatCtx.activeSkills()
	}
	if chat.IsSandboxed() {
		return nil
	}
	ids, err := a.ds.ListChatLoadedSkillIDs(ctx, chat.UserID, chat.ID)
	if err != nil || len(ids) == 0 {
		if err != nil {
			a.logger.Warn("context files: failed listing the chat's loaded skills", zap.String("chat_id", chat.ID.String()), zap.Error(err))
		}
		return nil
	}
	loaded, err := a.ds.GetRitualsByIDs(ctx, chat.UserID, ids)
	if err != nil {
		a.logger.Warn("context files: failed reading the chat's loaded skills", zap.String("chat_id", chat.ID.String()), zap.Error(err))
		return nil
	}
	return loaded
}

// recalledAttachments are the files a message's find_context calls returned chunks of.
func (a *Agent) recalledAttachments(ctx context.Context, chat *models.Chat, msg *models.ChatMessage) []*models.FileAttachment {
	if msg == nil {
		return nil
	}
	var out []*models.FileAttachment
	seen := map[uuid.UUID]bool{}
	for _, tc := range msg.ToolCalls {
		if tc == nil || tc.ToolName != tools.RecallToolSpec.Name || strings.TrimSpace(tc.ToolOutput) == "" {
			continue
		}
		var parsed struct {
			Chunks []struct {
				AttachmentID string `json:"attachment_id"`
			} `json:"chunks"`
		}
		if json.Unmarshal([]byte(tc.ToolOutput), &parsed) != nil {
			continue
		}
		for _, chunk := range parsed.Chunks {
			id, err := uuid.Parse(strings.TrimSpace(chunk.AttachmentID))
			if err != nil || id == uuid.Nil || seen[id] {
				continue
			}
			seen[id] = true
			if att, err := a.ds.GetFileAttachment(ctx, chat.UserID, id); err == nil && att != nil {
				out = append(out, att)
			}
		}
	}
	return out
}

// listChatMessagesForContext pages through the chat's messages (newest first) from minDate.
func (a *Agent) listChatMessagesForContext(ctx context.Context, chat *models.Chat, minDate *time.Time) ([]*models.ChatMessage, error) {
	const pageSize = 100
	const maxPages = 20
	filters := models.ChatMessageFilters{MinDate: minDate, SkipTotal: true}
	out := make([]*models.ChatMessage, 0, pageSize)
	for page := 1; page <= maxPages; page++ {
		rows, err := a.ds.ListChatMessages(ctx, chat.UserID, chat.ID, page, pageSize, filters)
		if err != nil {
			return nil, err
		}
		if rows == nil || len(rows.Results) == 0 {
			break
		}
		for _, item := range rows.Results {
			if msg, ok := item.(*models.ChatMessage); ok && msg != nil {
				out = append(out, msg)
			}
		}
		if len(rows.Results) < pageSize {
			break
		}
	}
	return out, nil
}

// ContextWorkspacePath is where a message attachment or agent file mounts in a container: its
// name, relative to /workspace, with traversal and absolute paths refused. The gallery folder is
// a label and plays no part (the rule the private build has always used).
func ContextWorkspacePath(att *models.FileAttachment) (string, error) {
	if att == nil {
		return "", fmt.Errorf("attachment is nil")
	}
	return validatedWorkspaceRelPath(att.Name)
}

// SkillBundleWorkspacePath is a bundle file's path inside the workspace: skills/<slug>/<rel>.
func SkillBundleWorkspacePath(slug, rel string) (string, error) {
	p, err := validatedWorkspaceRelPath(path.Join(models.ReservedSkillsFolder, slug, rel))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(p, models.ReservedSkillsFolder+"/"+slug+"/") {
		return "", fmt.Errorf("path escapes the skill's folder")
	}
	return p, nil
}

func validatedWorkspaceRelPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name is empty")
	}
	normalized := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("absolute path not allowed")
	}
	normalized = path.Clean(normalized)
	if normalized == "." || normalized == "" || normalized == "/" {
		return "", fmt.Errorf("path resolves to the workspace root")
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." || part == "" {
			return "", fmt.Errorf("path traversal or empty component not allowed")
		}
	}
	return normalized, nil
}

// contextFileCollector gathers files with their reasons, then resolves which takes each path.
type contextFileCollector struct {
	order []uuid.UUID
	byID  map[uuid.UUID]*ContextFile
	seq   map[uuid.UUID]int
	next  int
}

func newContextFileCollector() *contextFileCollector {
	return &contextFileCollector{byID: map[uuid.UUID]*ContextFile{}, seq: map[uuid.UUID]int{}}
}

// add records att under workspacePath (its name when empty) with reason; a file seen again only
// gains the reason. Files whose name makes no valid path are left out.
func (c *contextFileCollector) add(att *models.FileAttachment, workspacePath string, reason ContextReason) {
	if att == nil || att.ID == uuid.Nil {
		return
	}
	if existing, ok := c.byID[att.ID]; ok {
		for _, r := range existing.Reasons {
			if r == reason {
				return
			}
		}
		existing.Reasons = append(existing.Reasons, reason)
		return
	}
	if workspacePath == "" {
		p, err := ContextWorkspacePath(att)
		if err != nil {
			return
		}
		workspacePath = p
	}
	c.byID[att.ID] = &ContextFile{Attachment: att, WorkspacePath: workspacePath, Reasons: []ContextReason{reason}}
	c.seq[att.ID] = c.next
	c.next++
	c.order = append(c.order, att.ID)
}

// result resolves the files that share a path (the newest wins; equal times prefer the one seen
// later, as in-memory files of this turn carry no time) and returns them newest first.
func (c *contextFileCollector) result() []ContextFile {
	winner := map[string]uuid.UUID{}
	prefer := func(next, cur *models.FileAttachment) bool {
		return !next.CreatedAt.Before(cur.CreatedAt)
	}
	for _, id := range c.order {
		f := c.byID[id]
		key := strings.ToLower(f.WorkspacePath)
		if curID, ok := winner[key]; !ok || prefer(f.Attachment, c.byID[curID].Attachment) {
			winner[key] = id
		}
	}
	out := make([]ContextFile, 0, len(c.order))
	for _, id := range c.order {
		f := *c.byID[id]
		if w := winner[strings.ToLower(f.WorkspacePath)]; w != id {
			shadow := w
			f.ShadowedBy = &shadow
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Attachment, out[j].Attachment
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return c.seq[a.ID] > c.seq[b.ID]
	})
	return out
}
