#!/bin/sh -eu

# Exceed default hook log size of 1<<20 bytes by double
# The Go test should fail if buffer size exceeds 1MB

pre_testing() {
	ln=0
	while :; do printf '%06d: 012345678901234567890123456789012345678901234567890123456789012345678901\n' "$ln"; ln="$(($ln + 1))"; done \
	| head -c$(( 2 << 20 ))
}

case "$ZREPL_HOOKTYPE" in
	pre_testing)
		"$ZREPL_HOOKTYPE";;
	*)
		# Not handled by this script
		exit 0;;
esac
