#!/bin/sh -eu

if [ "$ZREPL_HOOKTYPE" = "pre_testing" ]; then
	>&2 echo "TEST ERROR $ZREPL_HOOKTYPE $ZREPL_FS@$ZREPL_SNAPNAME"
	exit 1
elif [ "$ZREPL_HOOKTYPE" = "post_testing" ]; then
	echo "TEST $ZREPL_HOOKTYPE $ZREPL_FS@$ZREPL_SNAPNAME"
else
	printf "Unknown hook type: %s" "$ZREPL_HOOKTYPE" 
	exit 255
fi
