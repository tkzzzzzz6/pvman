#!/usr/bin/env bash
#
# Summarize commits since the last v* tag, grouped by Conventional Commit type,
# as markdown suitable for a tag annotation or release notes draft.
#
#   summarize_changes.sh [--since <tag>] [--no-tag]
#
# Non-conventional subjects are listed under "Other" so nothing is silently
# dropped. Breaking changes are always called out first.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: summarize_changes.sh [--since <tag>] [--no-tag]

  --since <tag>   baseline tag (default: latest v* tag by version order)
  --no-tag        always use the whole history (useful before the first tag)
  -h, --help      show this help
EOF
}

die() { echo "error: $*" >&2; exit 1; }

since=
no_tag=0

while [ $# -gt 0 ]; do
  case "$1" in
    --since)
      since="${2:-}"
      [ -n "$since" ] || die "--since requires a tag"
      shift
      ;;
    --no-tag) no_tag=1 ;;
    -h | --help)
      usage
      exit 0
      ;;
    *) die "unknown argument: $1" ;;
  esac
  shift
done

git rev-parse --git-dir >/dev/null 2>&1 || die "not a git repository"

if [ "$no_tag" -eq 0 ] && [ -z "$since" ]; then
  since="$(git tag --list 'v[0-9]*' --sort=-v:refname | head -n 1)"
fi

if [ -n "$since" ]; then
  git rev-parse -q --verify "refs/tags/${since}" >/dev/null ||
    die "tag not found: ${since}"
  range="${since}..HEAD"
  heading="Changes since ${since}"
else
  range="HEAD"
  heading="Changes (no previous tag)"
fi

count="$(git rev-list --count --no-merges "$range" 2>/dev/null || echo 0)"
if [ "$count" -eq 0 ]; then
  echo "${heading}: no commits."
  exit 0
fi

echo "## ${heading}"
echo

# Breaking changes first — they drive the version bump.
breaking="$(
  git log --no-merges --format='%h %s' "$range" |
    grep -E '^[0-9a-f]+ [a-zA-Z]+(\([^)]*\))?!:' || true
)"
if [ -n "$breaking" ]; then
  echo "### Breaking"
  printf '%s\n' "$breaking" | sed 's/^/- /'
  echo
fi

emit_group() {
  local title="$1" pattern="$2" lines
  lines="$(
    git log --no-merges --format='%h %s' "$range" |
      grep -E "$pattern" || true
  )"
  if [ -n "$lines" ]; then
    echo "### ${title}"
    printf '%s\n' "$lines" | sed 's/^/- /'
    echo
  fi
}

emit_group "Features" '^[0-9a-f]+ feat(\([^)]*\))?:'
emit_group "Fixes" '^[0-9a-f]+ fix(\([^)]*\))?:'
emit_group "Performance" '^[0-9a-f]+ perf(\([^)]*\))?:'
emit_group "Refactor" '^[0-9a-f]+ refactor(\([^)]*\))?:'
emit_group "Documentation" '^[0-9a-f]+ docs(\([^)]*\))?:'
emit_group "Tests" '^[0-9a-f]+ test(\([^)]*\))?:'
emit_group "Build & CI" '^[0-9a-f]+ (build|ci)(\([^)]*\))?:'
emit_group "Chores" '^[0-9a-f]+ chore(\([^)]*\))?:'

# Anything without a recognized conventional prefix, minus the breaking ones
# already listed above.
other="$(
  git log --no-merges --format='%h %s' "$range" |
    grep -Ev '^[0-9a-f]+ (feat|fix|perf|refactor|docs|test|build|ci|chore)(\([^)]*\))?!?:' || true
)"
if [ -n "$other" ]; then
  echo "### Other"
  printf '%s\n' "$other" | sed 's/^/- /'
  echo
fi
