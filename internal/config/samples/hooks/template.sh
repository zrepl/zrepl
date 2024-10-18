#!/usr/bin/env bash
set -e

[ "$ZREPL_DRYRUN" = "true" ] && DRYRUN="echo DRYRUN: "

pre_snapshot() {
    $DRYRUN date
}

post_snapshot() {
    $DRYRUN date
}

case "$ZREPL_HOOKTYPE" in
    pre_snapshot|post_snapshot)
        "$ZREPL_HOOKTYPE"
        ;;
    *)
        printf 'Unrecognized hook type: %s\n' "$ZREPL_HOOKTYPE"
        exit 255
        ;;
esac
