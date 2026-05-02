#!/usr/bin/env bash
#
# Release script for gent.
#
# Dry-run is the default so release notes and commands can be reviewed before a tag is created.
# Use --execute when the checked output is ready to publish.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DEFAULT_VERSION="v0.1.0"
VERSION=""
EXECUTE=0
RUN_TESTS=1
ALLOW_DIRTY=0
REMOTE="origin"

usage() {
	cat <<'USAGE'
Usage:
  scripts/release.sh [version] [options]

Arguments:
  version        Go module version to release. Accepts 0.1.0 or v0.1.0.
                 Defaults to v0.1.0.

Options:
  --execute      Create the annotated tag, push it, and trigger Go proxy indexing.
  --skip-tests   Skip go test verification. Intended only for command-preview runs.
  --allow-dirty  Allow dry-run output when the worktree has uncommitted changes.
  --remote NAME  Git remote to push to. Defaults to origin.
  -h, --help     Show this help text.

Examples:
  scripts/release.sh v0.1.0
  scripts/release.sh v0.1.0 --execute
USAGE
}

fail() {
	printf 'Error: %s\n' "$1" >&2
	exit 1
}

warn() {
	printf 'Warning: %s\n' "$1" >&2
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--execute)
			EXECUTE=1
			shift
			;;
		--skip-tests)
			RUN_TESTS=0
			shift
			;;
		--allow-dirty)
			ALLOW_DIRTY=1
			shift
			;;
		--remote)
			[[ $# -ge 2 ]] || fail '--remote requires a value'
			REMOTE="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		-*)
			fail "unknown option: $1"
			;;
		*)
			[[ -z "$VERSION" ]] || fail 'version was provided more than once'
			VERSION="$1"
			shift
			;;
	esac
done

VERSION="${VERSION:-$DEFAULT_VERSION}"
if [[ "$VERSION" != v* ]]; then
	VERSION="v$VERSION"
fi

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
	fail "version must be valid Go semver, got $VERSION"
fi

cd "$REPO_ROOT"

[[ -f go.mod ]] || fail 'go.mod not found at repository root'

MODULE_PATH="$(awk '$1 == "module" {print $2; exit}' go.mod)"
[[ -n "$MODULE_PATH" ]] || fail 'could not read module path from go.mod'
CLI_PATH="$MODULE_PATH/cmd/gent"

verify_cli_build() {
	local tmp_bin
	tmp_bin="$(mktemp -d)"
	(
		trap 'rm -rf "$tmp_bin"' EXIT
		GOBIN="$tmp_bin" go install ./cmd/gent
		"$tmp_bin/gent" model list >/dev/null
	)
}

if git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null; then
	fail "tag $VERSION already exists locally"
fi

set +e
git ls-remote --exit-code --tags "$REMOTE" "refs/tags/$VERSION" >/dev/null 2>&1
REMOTE_TAG_STATUS=$?
set -e

if [[ "$REMOTE_TAG_STATUS" -eq 0 ]]; then
	fail "tag $VERSION already exists on remote $REMOTE"
fi
if [[ "$REMOTE_TAG_STATUS" -ne 2 ]]; then
	fail "could not check remote tag $VERSION on $REMOTE"
fi

WORKTREE_STATUS="$(git status --porcelain)"
if [[ -n "$WORKTREE_STATUS" ]]; then
	if [[ "$EXECUTE" -eq 1 ]]; then
		fail 'refusing to release from a dirty worktree'
	fi
	if [[ "$ALLOW_DIRTY" -eq 0 ]]; then
		warn 'worktree has uncommitted changes; use --allow-dirty to suppress this warning'
	fi
fi

CURRENT_BRANCH="$(git branch --show-current)"
if [[ "$CURRENT_BRANCH" != "main" ]]; then
	if [[ "$EXECUTE" -eq 1 ]]; then
		fail "release must be run from main, current branch is ${CURRENT_BRANCH:-detached}"
	fi
	warn "current branch is ${CURRENT_BRANCH:-detached}; release should run from main"
fi

UPSTREAM="$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null || true)"
if [[ -n "$UPSTREAM" ]]; then
	LOCAL_HEAD="$(git rev-parse @)"
	UPSTREAM_HEAD="$(git rev-parse '@{u}')"
	BASE_HEAD="$(git merge-base @ '@{u}')"
	if [[ "$LOCAL_HEAD" != "$UPSTREAM_HEAD" || "$LOCAL_HEAD" != "$BASE_HEAD" ]]; then
		if [[ "$EXECUTE" -eq 1 ]]; then
			fail "local branch is not up to date with $UPSTREAM"
		fi
		warn "local branch is not up to date with $UPSTREAM"
	fi
else
	warn 'current branch has no upstream; release execution will skip upstream parity checks'
fi

PREV_TAG="$({
	git tag -l 'v*' --sort=-version:refname |
		awk '/^v[0-9]+\.[0-9]+\.[0-9]+/ {print; exit}'
})"

if [[ -n "$PREV_TAG" ]]; then
	CHANGELOG="$(git log "$PREV_TAG"..HEAD --pretty=format:'- %s (%h)')"
else
	CHANGELOG="$(git log --pretty=format:'- %s (%h)')"
fi

if [[ -z "$CHANGELOG" ]]; then
	CHANGELOG='- No changes recorded'
fi

PREV_TAG_LABEL="${PREV_TAG:-"(none)"}"
MODE_LABEL="dry-run"
if [[ "$EXECUTE" -eq 1 ]]; then
	MODE_LABEL="execute"
fi

TAG_MESSAGE="gent $VERSION

Changes since ${PREV_TAG:-initial}:

$CHANGELOG"

printf '========================================\n'
printf '  gent Release Script\n'
printf '========================================\n\n'
printf 'Module:        %s\n' "$MODULE_PATH"
printf 'CLI package:   %s\n' "$CLI_PATH"
printf 'Version:       %s\n' "$VERSION"
printf 'New tag:       %s\n' "$VERSION"
printf 'Previous tag:  %s\n' "$PREV_TAG_LABEL"
printf 'Mode:          %s\n\n' "$MODE_LABEL"

printf '========================================\n'
printf '  Changelog\n'
printf '========================================\n\n'
printf '%s\n\n' "$CHANGELOG"

printf '========================================\n'
printf '  Tag Message Preview\n'
printf '========================================\n\n'
printf '%s\n\n' "$TAG_MESSAGE"

if [[ "$RUN_TESTS" -eq 1 ]]; then
	printf '========================================\n'
	printf '  Verification\n'
	printf '========================================\n\n'
	go test ./... -count=1 -timeout 300s
	verify_cli_build
	printf '\n'
else
	warn 'skipping tests by request'
fi

if [[ "$EXECUTE" -eq 0 ]]; then
	cat <<EOF
========================================
  Commands to create and push release
========================================

# Step 1: Verify tests and CLI build:
go test ./... -count=1 -timeout 300s
CLI_GOBIN="\$(mktemp -d)"
GOBIN="\$CLI_GOBIN" go install ./cmd/gent
"\$CLI_GOBIN/gent" model list >/dev/null
rm -rf "\$CLI_GOBIN"

# Step 2: Create the annotated tag:
git tag -a "$VERSION" -m "\$(cat <<'TAG_MESSAGE'
$TAG_MESSAGE
TAG_MESSAGE
)"

# Step 3: Push the tag to remote:
git push "$REMOTE" "$VERSION"

# Step 4: Trigger Go proxy to index the new version:
GOPROXY=https://proxy.golang.org go list -m "$MODULE_PATH@$VERSION"

# Step 5: Verify users can install the CLI from the released tag:
CLI_GOBIN="\$(mktemp -d)"
GOBIN="\$CLI_GOBIN" go install "$CLI_PATH@$VERSION"
"\$CLI_GOBIN/gent" model list >/dev/null
rm -rf "\$CLI_GOBIN"

# After indexing, users can depend on the library:
# go get $MODULE_PATH@$VERSION
# And install the CLI:
# go install $CLI_PATH@$VERSION
EOF
	exit 0
fi

printf '========================================\n'
printf '  Publishing\n'
printf '========================================\n\n'

git tag -a "$VERSION" -m "$TAG_MESSAGE"
git push "$REMOTE" "$VERSION"

if ! GOPROXY=https://proxy.golang.org go list -m "$MODULE_PATH@$VERSION"; then
	warn "Go proxy indexing did not complete; retry later:"
	warn "GOPROXY=https://proxy.golang.org go list -m $MODULE_PATH@$VERSION"
fi

CLI_GOBIN="$(mktemp -d)"
(
	trap 'rm -rf "$CLI_GOBIN"' EXIT
	GOBIN="$CLI_GOBIN" go install "$CLI_PATH@$VERSION"
	"$CLI_GOBIN/gent" model list >/dev/null
)

printf '\nRelease %s published.\n' "$VERSION"
printf 'Library: go get %s@%s\n' "$MODULE_PATH" "$VERSION"
printf 'CLI:     go install %s@%s\n' "$CLI_PATH" "$VERSION"
