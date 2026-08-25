---
name: gh-issue
description: >-
  File a bug report or feature request on the public what-iff GitHub repo with
  the `gh` CLI, matching the repo's issue forms (.github/ISSUE_TEMPLATE) so
  agent-filed issues look identical to web-filed ones. Use when asked to
  "file/open/create an issue", "report this bug", "track this as a feature
  request", or when a finding should be recorded for later in THIS repo. Not
  for Jira, Notion, or any private tracker — if the user's tooling or context
  points there (ticket keys like ABC-123, Notion pages, internal/customer
  work), use that instead; this skill only targets the public GitHub repo.
---

# File a GitHub issue (public repo only)

Everything here is world-readable forever. No internal hostnames, customer
names, private-repo paths, Jira keys, Notion links, or session URLs in the
title or body.

## 1. Guard

```bash
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
[ "$REPO" = theimaginaryfoundation/what-iff ] || { echo "not the public repo ($REPO); stop"; exit 1; }
```

Stop if `gh` is missing or unauthenticated (`gh auth status`); do not fall
back to the raw API, a browser, or another tracker.

## 2. Dedupe

```bash
gh issue list -S "<2-4 keywords> in:title" --state all -L 5 --json number,title,state
```

If one matches, comment there or report it to the user instead of filing.

## 3. File

Body headings must match the form labels exactly (GitHub renders form
submissions as `### <label>` sections; same headings → same look and same
searchability). Drop optional sections you have nothing for.

**Bug** (`-l bug`):

```bash
gh issue create -l bug -t "<symptom, ≤72 chars, no prefix>" -F - <<'BODY'
### Area

<one of: API / backend (Go) | Web app (web/app) | Data layer (ent, migrations, Postgres) | LLM providers / agent | CI / Docker / build | Docs | Other>

### What happened

<observed behavior + exact error/log lines, trimmed>

### What you expected

<...>

### Steps to reproduce

1. ...

### Version or commit

<git rev-parse --short HEAD, or tag>

### Environment

<OS, Go/Node, docker compose vs make run, provider, browser — only what matters>

### Additional context

<related issues/PRs as #N, suspected cause, workaround>
BODY
```

**Feature** (`-l enhancement`):

```bash
gh issue create -l enhancement -t "<outcome, ≤72 chars>" -F - <<'BODY'
### Area

<same list as above>

### Problem

<what is hard today, and for whom>

### Proposed solution

<what changes; API/UI/config shape if known>

### Alternatives considered

<...>

### Additional context

<links, prior art, willing to implement?>
BODY
```

Report the URL `gh` prints. Only add `-a @me`, `-m`, or extra labels if asked.

## 4. Fix in place

If the issue turns out wrong or incomplete, `gh issue edit <N> -t/-F` the
original so it reads correctly on its own — never append a correction comment.
