#!/usr/bin/env python3
"""Merge one section into a sticky E2E status PR comment.

Each e2e workflow owns one section, wrapped in its own markers, inside a
sticky comment identified by COMMENT_KEY. This script fetches that comment
(if any), replaces just the caller's section, and posts or patches the
result — so two workflows sharing a comment never overwrite each other,
even when both finish around the same time. Workflows with different
COMMENT_KEYs get entirely separate comments (the opt-in local-LLM run posts
its own, since it triggers on a different cadence than the mock suite).

Talks to the API with urllib rather than `gh`: this runs inside whatever
job called the action, and the e2e container image does not ship gh.
"""
import json
import os
import re
import sys
import urllib.request

SECTION_RE = re.compile(
    r"<!-- e2e-status:([\w-]+):(\d+) -->\n(.*?)\n<!-- /e2e-status:\1 -->",
    re.DOTALL,
)


def api(method, path, data=None):
    base = os.environ.get("GITHUB_API_URL", "https://api.github.com")
    req = urllib.request.Request(
        path if path.startswith("http") else base + path,
        method=method,
        headers={
            "Authorization": f"Bearer {os.environ['GH_TOKEN']}",
            "Accept": "application/vnd.github+json",
            "User-Agent": "e2e-status-comment",
        },
        data=json.dumps(data).encode() if data is not None else None,
    )
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read() or "null")
    except urllib.error.HTTPError as e:
        print(f"{method} {path}: HTTP {e.code}: {e.read().decode(errors='replace')}", file=sys.stderr)
        sys.exit(1)


def main():
    repo = os.environ["REPO"]
    pr = os.environ["PR"]
    section = os.environ["SECTION"]
    order = os.environ["ORDER"]
    body_file = os.environ["BODY_FILE"]
    top_marker = f"<!-- {os.environ.get('COMMENT_KEY') or 'e2e-status-comment'} -->"

    with open(body_file) as f:
        new_body = f.read().strip("\n")

    # 100 comments of headroom is plenty for sticky comments that get
    # updated in place rather than accumulating one per push.
    comments = api("GET", f"/repos/{repo}/issues/{pr}/comments?per_page=100")
    existing = None
    for c in comments:
        if c["body"].startswith(top_marker):
            existing = c

    sections = {}
    if existing:
        for m in SECTION_RE.finditer(existing["body"]):
            sid, sorder, sbody = m.group(1), int(m.group(2)), m.group(3)
            sections[sid] = (sorder, sbody)

    sections[section] = (int(order), new_body)

    blocks = [
        f"<!-- e2e-status:{sid}:{sorder} -->\n{sbody}\n<!-- /e2e-status:{sid} -->"
        for sid, (sorder, sbody) in sorted(sections.items(), key=lambda kv: kv[1][0])
    ]
    full_body = top_marker + "\n\n" + "\n\n".join(blocks) + "\n"

    if existing:
        api("PATCH", f"/repos/{repo}/issues/comments/{existing['id']}", {"body": full_body})
        print(f"updated comment {existing['id']} (section: {section})")
    else:
        api("POST", f"/repos/{repo}/issues/{pr}/comments", {"body": full_body})
        print(f"posted new comment (section: {section})")


if __name__ == "__main__":
    main()
