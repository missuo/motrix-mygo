#!/bin/sh
# Prints the section of CHANGELOG.md for a version, without its heading, and
# fails when there is none.
#
#   scripts/release-notes.sh 0.0.3
set -eu
version=${1:?usage: release-notes.sh <version>}
notes=$(awk -v version="$version" '
	/^## / {
		heading = $2
		gsub(/[\[\]]/, "", heading)
		printing = (heading == version)
		next
	}
	printing { print }
' CHANGELOG.md | sed -e :a -e '/./,$!d' -e '/^\n*$/{$d;N;ba' -e '}')
if [ -z "$notes" ]; then
	echo "CHANGELOG.md has no notes for $version" >&2
	exit 1
fi
printf '%s\n' "$notes"
