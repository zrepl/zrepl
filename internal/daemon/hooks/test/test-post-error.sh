#!/bin/sh -eu

post_testing() {
	>&2 echo "TEST ERROR $ZREPL_HOOKTYPE $ZREPL_FS@$ZREPL_SNAPNAME"

	exit 1
}

case "$ZREPL_HOOKTYPE" in
	post_testing)
		"$ZREPL_HOOKTYPE";;
	*)
		# Not handled by this script
		exit 0;;
esac
