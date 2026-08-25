---
name: gh-pr
description: >-
  Open a pull request on the public what-iff GitHub repo with the `gh` CLI,
  filling the repo's PR template (.github/PULL_REQUEST_TEMPLATE.md) so
  agent-filed PRs look identical to human-filed ones. Use when asked to
  "make/open/create/file the PR", "push this up and open a PR", or after
  finishing a branch of committed work that's ready for review. Applies to
  this repository only — if the work is in any other checkout, this skill
  does not apply; use that repository's own process instead.
---

# File a GitHub pull request (public repo only)

Everything here is world-readable forever. No internal hostnames, customer
names, private-repo paths, Jira keys, Notion links, or session URLs in the
title or body — and no `Generated with`/`Claude-Session` style attribution.

## 1. Guard

```bash
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
[ "$REPO" = theimaginaryfoundation/what-iff ] || { echo "not the public repo ($REPO); stop"; exit 1; }
BRANCH=$(git branch --show-current)
[ "$BRANCH" != main ] || { echo "on main; create a feature branch first"; exit 1; }
case "$BRANCH" in *claude*) echo "branch name contains 'claude': $BRANCH"; exit 1;; esac
```

## 2. Dedupe

```bash
gh pr list --head "$BRANCH" --json number,url,state
```

If one is already open for this branch, report its URL instead of filing a
second one.

## 3. Push and file

```bash
git push -u origin "$BRANCH"
```

Body sections must match the template headings exactly (`## Summary`,
`## Test plan`, `## Checklist`) — same headings render the same and keep the
diff against the template minimal for reviewers. Fill every checklist item
you actually verified with `[x]`; leave the rest `[ ]`, never delete them.

```bash
gh pr create -t "<summary, ≤72 chars, no trailing period>" -F - <<'BODY'
## Summary

- <what changed, in 1-3 bullets>

## Test plan

- [x] `make pre-commit`
- [ ] Frontend lint/test/build
- [x] Manually verified: <what you actually did>

## Checklist

- [x] No credentials, personal exports, generated local state, or production config committed
- [x] Public defaults still work without hosted services
- [ ] Updated architecture docs (only if this touches system boundaries)
- [ ] Regenerated the e2e SDK (only if `openapi.yaml` changed)
BODY
```

Report the URL `gh` prints. Only add `-a @me`, `-l`, `--base`, or `-d`
(draft) if asked.

## 4. Fix in place

If the description turns out wrong or incomplete after further review,
`gh pr edit <N> -t/-F` the original — never leave it standing and add a
"correction" comment.
