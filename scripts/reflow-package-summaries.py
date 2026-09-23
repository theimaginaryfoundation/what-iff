#!/usr/bin/env python3
"""One sentence per line for every internal/**/_PACKAGE_SUMMARY.md.

The package summaries grew bullets that were single 1-3k character lines, so any two PRs
touching the same bullet conflicted. Breaking long lines at sentence boundaries ("semantic
line breaks") keeps each edit to the sentence it changes.

Lossless for rendering: continuation lines are indented to the list item's content column,
so Markdown joins them back into the same paragraph. The script refuses to write a file
whose whitespace-normalized text would change. Only lines longer than LIMIT are touched;
a single long sentence is left alone. Fenced code blocks, headings and tables are skipped.

Usage:
  scripts/reflow-package-summaries.py            # rewrite files in place
  scripts/reflow-package-summaries.py --check    # exit 1 if any file needs reflowing
  scripts/reflow-package-summaries.py FILE...    # limit to specific files
"""
import pathlib
import re
import sys

LIMIT = 160
# Never treat these as sentence ends ("e.g. `Foo`" must stay on one line).
ABBREVIATIONS = ('e.g.', 'i.e.', 'vs.', 'etc.', 'cf.', 'approx.', 'No.')
CLOSERS = ')*"\''


def split_sentences(text):
    """Split at '.', '!' or '?' (plus closing ')', '**', quotes) followed by a space and an
    uppercase letter, backtick, '*', '(' or '['. Never inside inline code."""
    out, buf, in_code, i = [], [], False, 0
    while i < len(text):
        ch = text[i]
        buf.append(ch)
        if ch == '`':
            in_code = not in_code
            i += 1
            continue
        if in_code or ch not in '.!?':
            i += 1
            continue
        j = i + 1
        while j < len(text) and text[j] in CLOSERS:
            buf.append(text[j])
            j += 1
        at_boundary = j + 1 < len(text) and text[j] == ' ' and re.match(r'[A-Z`*(\[]', text[j + 1])
        sentence = ''.join(buf)
        if at_boundary and not sentence.rstrip(CLOSERS).endswith(ABBREVIATIONS):
            out.append(sentence.strip())
            buf = []
            i = j + 1
        else:
            i = j
    tail = ''.join(buf).strip()
    if tail:
        out.append(tail)
    return out


def reflow_line(line):
    stripped = line.lstrip()
    if len(line) <= LIMIT or stripped.startswith(('|', '#')):
        return [line]
    m = re.match(r'^(\s*(?:[-*+]|\d+\.)\s+)', line)
    lead = m.group(1) if m else re.match(r'^\s*', line).group(0)
    parts = split_sentences(line[len(lead):])
    if len(parts) <= 1:
        return [line]
    indent = ' ' * len(lead)
    return [lead + parts[0]] + [indent + p for p in parts[1:]]


def reflow(text):
    out, in_fence = [], False
    for line in text.split('\n'):
        if line.lstrip().startswith('```'):
            in_fence = not in_fence
            out.append(line)
            continue
        out.extend([line] if in_fence else reflow_line(line))
    return '\n'.join(out)


def normalize(text):
    return re.sub(r'\s+', ' ', text).strip()


def main(argv):
    check = '--check' in argv
    files = [pathlib.Path(a) for a in argv if a != '--check']
    if not files:
        files = sorted(pathlib.Path('internal').rglob('_PACKAGE_SUMMARY.md'))
    needs = []
    for path in files:
        raw = path.read_bytes().decode('utf-8')
        newline = '\r\n' if '\r\n' in raw else '\n'
        text = raw.replace('\r\n', '\n')
        new = reflow(text)
        if new == text:
            continue
        if normalize(new) != normalize(text):
            sys.exit(f'{path}: reflow would change rendered text; refusing to write')
        needs.append(path)
        if not check:
            path.write_bytes(new.replace('\n', newline).encode('utf-8'))
    if check and needs:
        print('These package summaries have long multi-sentence lines (one sentence per line, please):')
        for path in needs:
            print(f'  {path}')
        print('Fix with: make reflow-package-summaries')
        return 1
    if not check:
        print(f'reflowed {len(needs)} file(s)')
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
