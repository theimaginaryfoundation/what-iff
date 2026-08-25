---
name: address-pr-feedback
description: >-
  Turn the review feedback on a GitHub pull request into an organized,
  actionable triage plan in an isolated git worktree — without editing any code
  yet. Use whenever the user wants to address, respond to, work through, or
  "start on" the review comments / PR feedback / reviewer notes on a pull
  request (their most recent PR by default, or a given number) — even phrased
  loosely like "tackle the comments on my PR" or "organize the reviewer notes".
  Not for authoring new code, opening a PR, or reviewing an unpushed diff.
---

# Address PR Feedback

Produce a clean, prioritized **triage plan** of a PR's review feedback, staged in
a dedicated worktree so later fixes stay isolated. Stop before editing code — the
deliverable is an accurate map of what reviewers actually asked for.

The hard part is **signal vs. noise**: reviewer asks are spread across three
GitHub comment surfaces and buried in automated chatter (bot approvals, CI
tables, pasted config). A good plan surfaces every genuine ask exactly once with
its `file:line` anchor, and drops noise without discarding anything a human
wrote.

## Workflow

**1. Resolve + collect** with the bundled collector. It resolves the PR, then
returns PR meta, verdict, and all three comment surfaces (reviews, inline line
comments, conversation) as one JSON object — pre-filtered to drop pure noise
(regression tables, bare bot approvals) while keeping structured bot findings and
every human comment full.

```bash
scripts/collect.sh [<N>]   # no arg -> the user's most recent authored PR
```

Confirm the resolved PR number + title with the user before continuing. If
`gh auth status` fails, stop and have them run `gh auth login`.

**2. Create an isolated worktree** off the PR branch (`.pr.headRefName` from the
JSON) so the work doesn't disturb the user's checkout. Report the path; don't
`cd` their shell.

```bash
git fetch origin <headRefName>
git worktree add ../<repo>-pr<N>-feedback <headRefName>
```

If the branch is already checked out elsewhere, git refuses — report that rather
than forcing it.

**3. Triage** the collected JSON. Route every item by **author first, then
content** — this ordering is what keeps a casual human aside from being mistaken
for noise. The collector already dropped pure noise; the judgment below is what it
can't do.

*Human-authored comment* → it may ONLY land in 🔧 Actionable, 🧹 Nitpick, or
❓ Needs decision. A human comment is **never** noise or "informational", no
matter how casual, hedged, optional, or rambling ("you could delete this if you
want", "might be nice to…", a pasted config/persona). The ✅ bucket and any
"noise" label are off-limits for human content. Sort it:
- Clear, concrete request → 🔧 Actionable (or 🧹 Nitpick if trivial).
- Ambiguous, optional, hedged, or half-formed → ❓ Needs decision. Capture the
  underlying ask and let the user call it. (e.g. "you could delete deploy.sh
  if you want" is a **decision**, not noise.)
- The only human content with no action is an explicit approval carrying no ask
  ("LGTM") — record it as the verdict, not an item.

*Bot-authored comment* → a *structured* finding naming a concrete change with a
`file:line` anchor (e.g. review-bot "Minor"/"Nitpick") is 🔧 Actionable/🧹 Nitpick,
even though a bot wrote it. Drop duplicate postings of the same finding across
rounds (keep the most complete). Telling a bot's *approval spam* from its
*structured findings* is the main bot-side call — don't blanket-drop by author.

The ✅ "Likely addressed / informational" bucket is only for items confirmed
already done, or bot status — never a resting place for an unresolved human ask.
When unsure, prefer ❓ Needs decision: over-triaging is recoverable; dropping a
reviewer's request is not. Note `in_reply_to` chains that end in agreement as
likely-resolved.

**4. Present the plan.** Group by severity, most actionable first; keep each item
to one scannable line with a `file:line` anchor.

```markdown
## PR #<N> — <title>
Worktree: <path> · Reviewers: <humans> · Verdict: <APPROVED / CHANGES_REQUESTED>

### 🔧 Actionable — reviewer asks
- [ ] **<file>:<line>** — <ask> _(from @<author>)_
### 🧹 Nitpicks / optional
- [ ] **<file>:<line>** — <ask> _(from @<author>)_
### ❓ Needs your decision
- **<file/general>** — <ambiguous ask, briefly quoted> _(from @<author>)_ — why flagged: <what's unclear>
### ✅ Likely addressed / informational
- <item> — <why it's not an action>
```

End by asking which items to act on. Hand off to normal editing only once the
user chooses — this workflow makes **no** commits, pushes, or PR replies.

## Notes

- `scripts/collect.sh` is bundled with this skill; run it via `bash` (its output
  enters context, not its source). It is read-only.
- Non-Claude agents and humans can run the same collector and follow this file
  directly; `AGENTS.md` points here so tools that read it can find the workflow.
