#!/usr/bin/env bash
#
# build-diagrams-site.sh — build the static draw.io site the file workspace embeds (ADR 0x049).
#
# Downloads one pinned draw.io release (draw.war from jgraph/drawio), checks its SHA-256, unpacks
# its static web app into web/diagrams/dist, drops what a static embed does not use (the Java
# servlets, source maps, cloud integrations), and writes our settings (web/diagrams/PreConfig.js)
# over the release's own, and appends our bridge plugin (web/diagrams/whatiff-bridge.js) to its
# PostConfig.js. The site is served from an origin other than the app's.
#
# Usage: scripts/build-diagrams-site.sh <version> <sha256>   (the Makefile passes the pinned pair)
set -euo pipefail

VERSION="${1:?version}"
SHA256="${2:?sha256}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIR="$ROOT/web/diagrams"
CACHE="$DIR/.cache"
WAR="$CACHE/draw-$VERSION.war"
DIST="$DIR/dist"

mkdir -p "$CACHE"
if [[ ! -f "$WAR" ]]; then
  echo "downloading draw.io $VERSION"
  curl -fsSL -o "$WAR.part" "https://github.com/jgraph/drawio/releases/download/$VERSION/draw.war"
  mv "$WAR.part" "$WAR"
fi
actual="$(sha256sum "$WAR" | cut -d' ' -f1)"
if [[ "$actual" != "$SHA256" ]]; then
  echo "error: draw.war $VERSION has SHA-256 $actual, expected $SHA256" >&2
  rm -f "$WAR"
  exit 1
fi
if [[ ! -f "$CACHE/LICENSE-$VERSION" ]]; then
  curl -fsSL -o "$CACHE/LICENSE-$VERSION" "https://raw.githubusercontent.com/jgraph/drawio/$VERSION/LICENSE"
fi

rm -rf "$DIST"
mkdir -p "$DIST"
python3 -m zipfile -e "$WAR" "$DIST"
# Not part of a static embed: the Java web app, source maps, and the cloud and chat integrations.
rm -rf "$DIST/WEB-INF" "$DIST/META-INF" "$DIST/connect" "$DIST/js/onedrive" \
  "$DIST/teams.html" "$DIST/onedrive3.html" "$DIST/monday-app-association.json"
find "$DIST" -name '*.map' -delete
cp "$DIR/PreConfig.js" "$DIST/js/PreConfig.js"
# Our bridge (comments on elements) runs as a draw.io plugin, loaded with the release's PostConfig.
{ printf '\n'; cat "$DIR/whatiff-bridge.js"; } >> "$DIST/js/PostConfig.js"
cp "$CACHE/LICENSE-$VERSION" "$DIST/LICENSE"
cat > "$DIST/NOTICE" <<NOTICE
This site is draw.io $VERSION (https://github.com/jgraph/drawio), Apache License 2.0 (see LICENSE),
built by What If's scripts/build-diagrams-site.sh with its own js/PreConfig.js.
draw.io is a trademark of JGraph Holdings Ltd; its icons and shape libraries have their own terms.
NOTICE
echo "built draw.io $VERSION into ${DIST#$ROOT/} ($(du -sh "$DIST" | cut -f1))"
