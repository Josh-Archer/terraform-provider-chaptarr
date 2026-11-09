#!/usr/bin/env bash
# Resolve relative links in the consumer-facing docs and release notes.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

python3 - "${repo_root}" <<'PY'
import re
import sys
from pathlib import Path

root = Path(sys.argv[1])
files = [root / "README.md", root / "CONTRIBUTING.md", root / "SECURITY.md"]
docs = root / "docs"
files.extend(sorted(docs.rglob("*.md")))

link_re = re.compile(r"!?\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
heading_re = re.compile(r"^#{1,6}\s+(.*?)\s*#*\s*$")
explicit_id_re = re.compile(r"""\bid=["']([^"']+)["']""")

def slugify(title):
    title = re.sub(r"<[^>]+>", "", title)
    title = title.strip().lower()
    title = re.sub(r"[^\w\s-]", "", title, flags=re.UNICODE)
    title = re.sub(r"[-\s]+", "-", title).strip("-")
    return title

def visible_lines(text):
    in_fence = False
    for lineno, line in enumerate(text.splitlines(), 1):
        if line.startswith("```"):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        yield lineno, line

def anchors(text):
    found = set()
    seen = {}
    for _, line in visible_lines(text):
        for match in explicit_id_re.finditer(line):
            found.add(match.group(1))
        heading = heading_re.match(line.strip())
        if not heading:
            continue
        slug = slugify(heading.group(1))
        if not slug:
            continue
        count = seen.get(slug, 0)
        seen[slug] = count + 1
        found.add(slug if count == 0 else f"{slug}-{count}")
    return found

anchor_cache = {}

def file_anchors(path):
    cached = anchor_cache.get(path)
    if cached is None:
        cached = anchors(path.read_text(encoding="utf-8"))
        anchor_cache[path] = cached
    return cached

missing = []
checked = 0
for path in files:
    text = path.read_text(encoding="utf-8")
    for lineno, line in visible_lines(text):
        for match in link_re.finditer(line):
            raw = match.group(1)
            if raw.startswith(("http://", "https://", "mailto:", "javascript:")):
                continue
            target_path, _, fragment = raw.partition("#")
            if target_path == "":
                target = path
            else:
                target = (path.parent / target_path).resolve()
            checked += 1
            if not target.exists():
                missing.append(f"{path.relative_to(root)}:{lineno}: missing {raw}")
                continue
            if fragment and target.is_file():
                if fragment not in file_anchors(target):
                    missing.append(f"{path.relative_to(root)}:{lineno}: missing anchor {raw}")

if missing:
    print("Broken documentation links:", file=sys.stderr)
    for item in missing:
        print(item, file=sys.stderr)
    sys.exit(1)

print(f"Documentation links: {checked} relative links resolved")
PY
