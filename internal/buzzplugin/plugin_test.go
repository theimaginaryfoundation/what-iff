package buzzplugin

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// A fresh connection (another binding changing, a deploy, a new leader) resumes each channel from
// the newest event seen in it, not from now, so mentions sent while it was down are still
// answered.
func TestResumeMarks(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	id := uuid.New()
	var m resumeMarks
	assert.False(t, m.note(id, now.Unix(), now), "a binding the gateway is not listening to keeps no mark")
	m.listed([]uuid.UUID{id})

	assert.Equal(t, now, m.start(id, nil, now), "nothing seen yet: now")
	stored := now.Add(-10 * time.Minute)
	assert.Equal(t, stored, m.start(id, &stored, now), "across a restart: the binding row")
	ancient := now.Add(-72 * time.Hour)
	assert.Equal(t, now.Add(-maxResume), m.start(id, &ancient, now), "no further back than maxResume")

	assert.True(t, m.note(id, now.Add(-5*time.Minute).Unix(), now), "the first event is written")
	assert.False(t, m.note(id, now.Add(-4*time.Minute).Unix(), now.Add(time.Second)), "then at most every markEvery")
	assert.Equal(t, now.Add(-4*time.Minute), m.start(id, &stored, now), "across a re-dial: the newest in memory")
	assert.False(t, m.note(id, now.Add(-6*time.Minute).Unix(), now.Add(time.Hour)), "older events do not move it back")
	assert.False(t, m.note(id, now.Add(time.Hour).Unix(), now), "nor do events from the future")
	assert.True(t, m.note(id, now.Add(-time.Minute).Unix(), now.Add(markEvery)), "written again after markEvery")
	assert.Equal(t, now, m.start(uuid.New(), nil, now), "per binding")

	// A binding the gateway stops listening to (a pause, inbound off) loses its mark, so
	// listening again does not answer what was said meanwhile.
	other := uuid.New()
	assert.Empty(t, m.listed([]uuid.UUID{id, other}))
	assert.Equal(t, []uuid.UUID{id}, m.listed([]uuid.UUID{other}))
	assert.Equal(t, now, m.start(id, nil, now), "forgotten")
	assert.False(t, m.note(id, now.Unix(), now.Add(time.Hour)), "a late event from the closing session does not bring it back")
	// Until its row is cleared, the stored mark is not trusted either, and clearing is retried.
	assert.Equal(t, now, m.start(id, &stored, now), "a stored mark awaiting its clear is ignored")
	assert.Equal(t, []uuid.UUID{id}, m.listed([]uuid.UUID{id, other}), "retried at the next listing")
	m.cleared([]uuid.UUID{id})
	assert.Empty(t, m.listed([]uuid.UUID{id, other}), "listening again is not a stop")
	assert.Equal(t, stored, m.start(id, &stored, now), "a cleared binding trusts its row again")
}
