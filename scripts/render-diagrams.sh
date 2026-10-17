#!/bin/sh
# Render PlantUML sources to SVG.
# Usage:
#   scripts/render-diagrams.sh                    every .puml under docs/, to an SVG next to it
#   scripts/render-diagrams.sh <in.puml> <out.svg>  one source, to the given SVG path
#   scripts/render-diagrams.sh --check <base>     render the diagrams under docs/ that differ from <base>, and compare with the SVGs; changes no file
# Copied from cloveclovedev/cumin-works scripts/render-diagrams.sh at commit
# f1e4d22f9ea85a299828b1d8ff7f08c7d9eea3b4, written by cumin-trace init.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
image="cumin-plantuml:1.2025.4"

if [ $# -ne 0 ] && [ $# -ne 2 ]; then
  sed -n '2,6p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 2
fi

if [ "${1:-}" = "--check" ]; then
  # The diagrams that differ between <base> and the work tree: a .puml or its
  # SVG, with the files that git does not track yet. A change of the renderer
  # (tools/plantuml/) can change every SVG, so it selects every diagram.
  base="$2"
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  git -C "$root" -c core.quotePath=false diff --name-only "$base" -- docs tools/plantuml >"$tmp/changed"
  git -C "$root" -c core.quotePath=false ls-files --others --exclude-standard -- docs tools/plantuml >>"$tmp/changed"
  if grep -q '^tools/plantuml/' "$tmp/changed"; then
    (cd "$root" && find docs -name '*.puml') | sort >"$tmp/sources"
  else
    sed -n -e 's/^\(docs\/.*\)\.puml$/\1.puml/p' -e 's/^\(docs\/.*\)\.svg$/\1.puml/p' "$tmp/changed" | sort -u |
      while IFS= read -r src; do
        # An SVG with no .puml next to it is not a diagram of this script.
        [ ! -f "$root/$src" ] || echo "$src"
      done >"$tmp/sources"
  fi
  if [ ! -s "$tmp/sources" ]; then
    echo "no diagram differs from $base"
    exit 0
  fi

  docker build --quiet --tag "$image" "$root/tools/plantuml" >/dev/null

  # The source directory is read-only and the SVG goes to a temporary
  # directory, so the work tree stays as it is.
  mkdir "$tmp/out"
  status=0
  while IFS= read -r src; do
    svg="${src%.puml}.svg"
    out="$tmp/out/$(basename "$svg")"
    docker run --rm --volume "$root/$(dirname "$src"):/work:ro" --volume "$tmp/out:/out" "$image" -tsvg -charset UTF-8 -o /out "/work/$(basename "$src")" </dev/null
    if [ ! -f "$root/$svg" ]; then
      echo "missing: $svg" >&2
      status=1
    elif cmp -s "$out" "$root/$svg"; then
      echo "equal $svg"
    else
      echo "differs from the render: $svg" >&2
      status=1
    fi
    rm -f "$out"
  done <"$tmp/sources"
  exit "$status"
fi

docker build --quiet --tag "$image" "$root/tools/plantuml" >/dev/null

if [ $# -eq 2 ]; then
  # One diagram outside docs/, for example the diagram of an issue. The source
  # goes in and the SVG comes out through the pipe, so any path works.
  [ -f "$1" ] || { echo "error: no such file: $1" >&2; exit 1; }
  docker run --rm -i "$image" -tsvg -charset UTF-8 -pipe <"$1" >"$2"
  echo "rendered $2"
  exit 0
fi

find "$root/docs" -name '*.puml' | while read -r src; do
  dir="$(dirname "$src")"
  docker run --rm --volume "$dir:/work" "$image" -tsvg -charset UTF-8 "/work/$(basename "$src")"
  echo "rendered ${src#"$root"/}"
done
