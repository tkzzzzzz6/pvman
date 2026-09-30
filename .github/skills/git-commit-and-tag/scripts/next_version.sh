#!/usr/bin/env bash
#
# Compute the next semantic version tag for pvman.
#
#   next_version.sh [auto|major|minor|patch] [--since <tag>] [--dry-run]
#
# Bump policy for `auto`:
#   - BREAKING CHANGE (or `type!:`) -> major when major > 0, otherwise minor
#     (pvman is pre-1.0, where semver allows breaking changes in minor bumps)
#   - any `feat:` commit            -> minor
#   - everything else               -> patch
#
# The new version is written to stdout; diagnostics go to stderr so the result
# stays pipeable:  next="$(next_version.sh auto)"
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: next_version.sh [auto|major|minor|patch] [--since <tag>] [--dry-run]

  auto (default)      infer the bump from Conventional Commits since the last tag
  major|minor|patch   force a specific bump

Options:
  --since <tag>   baseline tag (default: latest v* tag by version order)
  --dry-run       also report the range, commit count and chosen bump on stderr
  -h, --help      show this help
EOF
}

die() { echo "error: $*" >&2; exit 1; }

bump=auto
since=
dry_run=0

while [ $# -gt 0 ]; do
  case "$1" in
    auto | major | minor | patch) bump="$1" ;;
    --since)
      since="${2:-}"
      [ -n "$since" ] || die "--since requires a tag"
      shift
      ;;
    --dry-run) dry_run=1 ;;
    -h | --help)
      usage
      exit 0
      ;;
    *) die "unknown argument: $1" ;;
  esac
  shift
done

git rev-parse --git-dir >/dev/null 2>&1 || die "not a git repository"

# Only v* tags participate: that is what release.yml reacts to.
if [ -z "$since" ]; then
  since="$(git tag --list 'v[0-9]*' --sort=-v:refname | head -n 1)"
fi

if [ -n "$since" ]; then
  git rev-parse -q --verify "refs/tags/${since}" >/dev/null ||
    die "tag not found: ${since}"
  range="${since}..HEAD"
  current="${since#v}"
else
  range="HEAD"
  current="0.0.0"
fi

IFS='.' read -r major minor patch <<<"$current"
for part in "$major" "$minor" "$patch"; do
  case "$part" in
    '' | *[!0-9]*) die "cannot parse version from tag: ${since:-<none>}" ;;
  esac
done

subject_log() { git log --no-merges --format='%s' "$range"; }
full_log() { git log --no-merges --format='%B' "$range"; }

has_breaking() {
  full_log | grep -Eq '(^|[[:space:]])BREAKING[ -]CHANGE:' && return 0
  subject_log | grep -Eq '^[a-zA-Z]+(\([^)]*\))?!:' && return 0
  return 1
}

commit_count="$(git rev-list --count --no-merges "$range" 2>/dev/null || echo 0)"

if [ "$bump" = auto ]; then
  if has_breaking; then
    if [ "$major" -eq 0 ]; then
      bump=minor
    else
      bump=major
    fi
  elif subject_log | grep -Eq '^feat(\([^)]*\))?:'; then
    bump=minor
  else
    bump=patch
  fi
fi

case "$bump" in
  major)
    major=$((major + 1))
    minor=0
    patch=0
    ;;
  minor)
    minor=$((minor + 1))
    patch=0
    ;;
  patch) patch=$((patch + 1)) ;;
esac

next="v${major}.${minor}.${patch}"

# Never propose a version that already exists: keep bumping patch.
while git rev-parse -q --verify "refs/tags/${next}" >/dev/null; do
  echo "warning: ${next} already exists, bumping patch" >&2
  patch=$((patch + 1))
  next="v${major}.${minor}.${patch}"
done

if [ "$dry_run" -eq 1 ]; then
  {
    echo "baseline : ${since:-<no tag>}"
    echo "range    : ${range}"
    echo "commits  : ${commit_count}"
    echo "bump     : ${bump}"
    echo "next     : ${next}"
  } >&2
  if [ "$commit_count" -eq 0 ]; then
    echo "warning: no commits since baseline, ${next} would tag the same tree" >&2
  fi
fi

printf '%s\n' "$next"
