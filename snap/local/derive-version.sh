#!/bin/sh
# derive-version.sh <project-dir> [dirty]
#
# Prints the snap version for the git checkout at <project-dir>, derived from the
# nearest release tag (v<version>), and fails when the result is not a valid snap
# version. Called by the go-cli part's override-build; runnable on its own to check
# what a build would produce.
#
#   on tag v1.0.0                  -> 1.0.0
#   3 commits after v1.0.0         -> 1.0.0+git3.abc1234
#   no v* tag reachable            -> 0.0.0+git.abc1234
#   not a git checkout             -> 0.0.0
#   second argument "dirty"        -> any of the above with "-dirty" appended
set -eu

dir="${1:?usage: derive-version.sh <project-dir> [dirty]}"
dirty="${2:-}"

project_git() { git -c safe.directory='*' -C "${dir}" "$@"; }

describe="$(project_git describe --tags --long --abbrev=7 --match 'v[0-9]*' 2>/dev/null || true)"
if [ -n "${describe}" ]; then
	# v1.0.0-3-gabc1234: tag, commits since the tag, abbreviated commit.
	base="$(printf '%s' "${describe}" | sed -E 's/^v(.+)-[0-9]+-g[0-9a-f]+$/\1/')"
	count="$(printf '%s' "${describe}" | sed -E 's/^.*-([0-9]+)-g[0-9a-f]+$/\1/')"
	sha="$(printf '%s' "${describe}" | sed -E 's/^.*-g([0-9a-f]+)$/\1/')"
	if [ "${count}" = 0 ]; then
		version="${base}"
	else
		version="${base}+git${count}.${sha}"
	fi
elif sha="$(project_git rev-parse --short=7 HEAD 2>/dev/null)"; then
	version="0.0.0+git.${sha}"
else
	version="0.0.0"
fi

if [ "${dirty}" = dirty ]; then
	version="${version}-dirty"
fi

# snapd: at most 32 characters from [A-Za-z0-9:.+~-], starting with an alphanumeric
# and not ending in ":" "." or "-". This is the stricter subset we produce.
if ! printf '%s' "${version}" | grep -Eq '^[A-Za-z0-9]([A-Za-z0-9.+~-]{0,30}[A-Za-z0-9])?$'; then
	echo "derive-version.sh: derived version \"${version}\" is not a valid snap version" >&2
	exit 1
fi

printf '%s\n' "${version}"
