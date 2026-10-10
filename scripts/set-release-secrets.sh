#!/bin/sh
# Sets the secrets the release workflow signs and notarizes the macOS app
# with, from a .p12 certificate and a file of KEY=value lines naming
# APPLE_ID, APPLE_TEAM_ID, APPLE_APP_PASSWORD and MACOS_CERTIFICATE_PASSWORD.
# Neither file belongs in the repository.
#
#   scripts/set-release-secrets.sh certificate.p12 credentials.txt [owner/repo]
set -eu

if [ $# -lt 2 ]; then
	echo "usage: $0 certificate.p12 credentials.txt [owner/repo]" >&2
	exit 2
fi
p12=$1
credentials=$2
repo=${3:+--repo $3}

value() {
	sed -n "s/^$1=//p" "$credentials" | head -n 1 | tr -d '\r'
}

for key in APPLE_ID APPLE_TEAM_ID APPLE_APP_PASSWORD MACOS_CERTIFICATE_PASSWORD; do
	v=$(value "$key")
	if [ -z "$v" ]; then
		echo "$credentials has no $key" >&2
		exit 1
	fi
	# shellcheck disable=SC2086
	printf '%s' "$v" | gh secret set "$key" $repo
done
# shellcheck disable=SC2086
base64 < "$p12" | tr -d '\n' | gh secret set MACOS_CERTIFICATE $repo
