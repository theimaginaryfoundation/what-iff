package datastore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ImportMessageOrderRepairResult records the narrowly scoped historical repair outcome.
// Ambiguous timestamp groups are filtered out before becoming candidates; collision and
// concurrent-change abstentions are counted explicitly because they were otherwise eligible.
type ImportMessageOrderRepairResult struct {
	CandidatePairs        int
	RepairedPairs         int
	CollisionAbstentions  int
	ConcurrentAbstentions int
}

type importedMessageOrderRepairCandidate struct {
	chatID uuid.UUID
	sentAt time.Time
}

// RepairImportedMessageOrder repairs historical import rows only where conversational order is
// causally recoverable: an archived imported chat contains exactly one User and one Assistant row
// at the exact same timestamp, the chat has no associated agent job, and moving the Assistant by
// one production-representable ordering step cannot collide with surrounding history.
//
// The normal Ent mutation API deliberately keeps ChatMessage.sent_at immutable. This repair uses a
// tightly scoped SQL transaction instead of weakening that invariant for every caller.
func (d *Datastore) RepairImportedMessageOrder(ctx context.Context) (ImportMessageOrderRepairResult, error) {
	result := ImportMessageOrderRepairResult{}

	tx, err := d.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin imported message order repair: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT cm.chat_messages, cm.sent_at
		FROM chat_messages AS cm
		JOIN chats AS c ON c.id = cm.chat_messages
		WHERE c.archived = TRUE
		  AND c.source IN ($1, $2)
		  AND c.import_hash IS NOT NULL
		  AND c.import_hash <> ''
		  AND NOT EXISTS (
			SELECT 1 FROM agent_jobs AS j WHERE j.chat_agent_jobs = c.id
		  )
		GROUP BY cm.chat_messages, cm.sent_at
		HAVING COUNT(*) = 2
		   AND SUM(CASE WHEN cm.origin = 'User' THEN 1 ELSE 0 END) = 1
		   AND SUM(CASE WHEN cm.origin = 'Assistant' THEN 1 ELSE 0 END) = 1
	`, models.ChatSourceOpenAI, models.ChatSourceAnthropic)
	if err != nil {
		return result, fmt.Errorf("find imported message order repair candidates: %w", err)
	}

	candidates := make([]importedMessageOrderRepairCandidate, 0)
	for rows.Next() {
		var candidate importedMessageOrderRepairCandidate
		if err := rows.Scan(&candidate.chatID, &candidate.sentAt); err != nil {
			_ = rows.Close()
			return result, fmt.Errorf("scan imported message order repair candidate: %w", err)
		}
		candidate.sentAt = candidate.sentAt.UTC()
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return result, fmt.Errorf("iterate imported message order repair candidates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return result, fmt.Errorf("close imported message order repair candidates: %w", err)
	}
	result.CandidatePairs = len(candidates)

	for _, candidate := range candidates {
		target := candidate.sentAt.Add(importedMessageOrderStep)

		var nextTime time.Time
		err := tx.QueryRowContext(ctx, `
			SELECT sent_at
			FROM chat_messages
			WHERE chat_messages = $1 AND sent_at > $2
			ORDER BY sent_at ASC, id ASC
			LIMIT 1
		`, candidate.chatID, candidate.sentAt).Scan(&nextTime)
		if err != nil && err != sql.ErrNoRows {
			return result, fmt.Errorf("check imported message order repair collision: %w", err)
		}
		if err == nil && !nextTime.UTC().After(target) {
			result.CollisionAbstentions++
			continue
		}

		var assistantID uuid.UUID
		if err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM chat_messages
			WHERE chat_messages = $1 AND sent_at = $2 AND origin = 'Assistant'
			ORDER BY id ASC
			LIMIT 1
		`, candidate.chatID, candidate.sentAt).Scan(&assistantID); err != nil {
			return result, fmt.Errorf("find assistant row for imported message order repair: %w", err)
		}

		update, err := tx.ExecContext(ctx, `
			UPDATE chat_messages
			SET sent_at = $1
			WHERE id = $2 AND sent_at = $3 AND origin = 'Assistant'
		`, target, assistantID, candidate.sentAt)
		if err != nil {
			return result, fmt.Errorf("repair imported assistant timestamp: %w", err)
		}
		affected, err := update.RowsAffected()
		if err != nil {
			return result, fmt.Errorf("read imported message order repair result: %w", err)
		}
		if affected != 1 {
			result.ConcurrentAbstentions++
			continue
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE chats
			SET last_message_time = $1
			WHERE id = $2 AND (last_message_time IS NULL OR last_message_time < $1)
		`, target, candidate.chatID); err != nil {
			return result, fmt.Errorf("update repaired import last message time: %w", err)
		}
		result.RepairedPairs++
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit imported message order repair: %w", err)
	}
	return result, nil
}
