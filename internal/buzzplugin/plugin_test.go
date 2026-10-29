package buzzplugin

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// A fresh connection (a changed binding, a deploy, a new leader) resumes from the newest event
// seen before, not from now, so mentions sent while it was down are still answered.
func TestResumeMarks(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	id := uuid.New()
	var m resumeMarks

	assert.Equal(t, now, m.start(id, nil, now), "nothing seen yet: now")
	stored := now.Add(-10 * time.Minute)
	assert.Equal(t, stored, m.start(id, &stored, now), "across a restart: the community row")
	ancient := now.Add(-72 * time.Hour)
	assert.Equal(t, now.Add(-maxResume), m.start(id, &ancient, now), "no further back than maxResume")

	assert.True(t, m.note(id, now.Add(-5*time.Minute).Unix(), now), "the first event is written")
	assert.False(t, m.note(id, now.Add(-4*time.Minute).Unix(), now.Add(time.Second)), "then at most every markEvery")
	assert.Equal(t, now.Add(-4*time.Minute), m.start(id, &stored, now), "across a re-dial: the newest in memory")
	assert.False(t, m.note(id, now.Add(-6*time.Minute).Unix(), now.Add(time.Hour)), "older events do not move it back")
	assert.False(t, m.note(id, now.Add(time.Hour).Unix(), now), "nor do events from the future")
	assert.True(t, m.note(id, now.Add(-time.Minute).Unix(), now.Add(markEvery)), "written again after markEvery")
	assert.Equal(t, now, m.start(uuid.New(), nil, now), "per community")
}
