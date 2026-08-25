#!/usr/bin/env bash
# Collect PR review feedback from all three GitHub surfaces, pre-filtered to
# drop marker-identifiable pure noise (regression tables, bare bot approvals) so
# the caller reads signal, not chatter. Prints one compact JSON object.
#
# Usage: collect.sh [<pr-number>]
#   No argument -> resolves the user's most recent authored PR.
#
# Noise dropped here is ONLY pure status noise (review-bot regression tables and
# bare "Approved by review-bot" verdicts). Structured bot findings (Minor/Nitpick)
# and every human comment are kept FULL — triage judgment stays with the caller.
set -euo pipefail

N="${1:-}"
if [ -z "$N" ]; then
  N=$(gh pr list --author "@me" --state all --limit 1 --json number --jq '.[0].number')
fi
if [ -z "$N" ]; then
  echo '{"error":"no PR found for the current user"}' >&2
  exit 1
fi

meta=$(gh pr view "$N" --json number,title,headRefName,state,url,reviewDecision)

# reviews: drop bot approval boilerplate + empty-body approvals; keep substantive
# (e.g. CHANGES_REQUESTED prose). Verdict itself is carried in meta.reviewDecision.
reviews=$(gh pr view "$N" --json reviews --jq \
  '[.reviews[]
    | select((.body|test("Approved by review-bot|^LGTM|^✅ Code review complete"))|not)
    | select(.body != "")
    | {author:.author.login, state:.state, body:.body}]')

# inline review-thread comments (anchored to file+line): kept full.
inline=$(gh api "repos/{owner}/{repo}/pulls/$N/comments" --jq \
  '[.[] | {author:.user.login, path:.path, line:.line, body:.body, in_reply_to:.in_reply_to_id}]')

# conversation comments: drop regression tables and bare approval posts; keep
# structured bot findings and human comments full.
conversation=$(gh pr view "$N" --json comments --jq \
  '[.comments[]
    | select((.body|test("review-bot:regression"))|not)
    | select((.body|test("^✅ Code review complete"))|not)
    | {author:.author.login, body:.body}]')

jq -n \
  --argjson meta "$meta" \
  --argjson reviews "$reviews" \
  --argjson inline "$inline" \
  --argjson conversation "$conversation" \
  '{pr:$meta, reviews:$reviews, inline_comments:$inline, conversation:$conversation}'
