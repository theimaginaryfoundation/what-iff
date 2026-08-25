#!/usr/bin/env python3
"""Merge one section into the shared E2E status PR comment.

Each e2e workflow owns one section, wrapped in its own markers, inside a
single sticky comment. This script fetches that comment (if any), replaces
just the caller's section, and posts or patches the result — so a mock-run
comment and a local-llm-run comment never overwrite each other, even when
both workflows finish around the same time.
"""
import json
import os
import re
import subprocess
import sys

TOP_MARKER = "<!-- e2e-status-comment -->"
SECTION_RE = re.compile(
    r"<!-- e2e-status:([\w-]+):(\d+) -->\n(.*?)\n<!-- /e2e-status:\1 -->",
    re.DOTALL,
)


def run(args, **kwargs):
    result = subprocess.run(args, capture_output=True, text=True, **kwargs)
    if result.returncode != 0:
        print(result.stderr, file=sys.stderr)
        sys.exit(1)
    return result.stdout


def main():
    repo = os.environ["REPO"]
    pr = os.environ["PR"]
    section = os.environ["SECTION"]
    order = os.environ["ORDER"]
    body_file = os.environ["BODY_FILE"]

    with open(body_file) as f:
        new_body = f.read().strip("\n")

    # Most recent 30 comments is enough headroom for a sticky comment that
    # gets updated in place rather than accumulating one per push.
    comments = json.loads(run(["gh", "api", f"repos/{repo}/issues/{pr}/comments"]))
    existing = None
    for c in comments:
        if c["body"].startswith(TOP_MARKER):
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
    full_body = TOP_MARKER + "\n\n" + "\n\n".join(blocks) + "\n"

    out_path = "/tmp/e2e-status-comment.md"
    with open(out_path, "w") as f:
        f.write(full_body)

    if existing:
        run(["gh", "api", "-X", "PATCH", f"repos/{repo}/issues/comments/{existing['id']}",
             "-F", f"body=@{out_path}"])
        print(f"updated comment {existing['id']} (section: {section})")
    else:
        run(["gh", "api", "-X", "POST", f"repos/{repo}/issues/{pr}/comments",
             "-F", f"body=@{out_path}"])
        print(f"posted new comment (section: {section})")


if __name__ == "__main__":
    main()
