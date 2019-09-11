#!/usr/bin/env bash
set -euo pipefail
set -x

POOLNAME="zreplplatformtest"
POOLIMG="$1"

if zpool status "$POOLNAME"; then
    exit 1
fi

if [ -e "$POOLIMG" ]; then
    exit 1
fi
fallocate -l 500M "$POOLIMG"
zpool create "$POOLNAME" "$POOLIMG"

SRC_DATASET="$POOLNAME/src"
DST_ROOT="$POOLNAME/dst"

keylocation="$(mktemp /tmp/ZREPL_PLATFORMTEST_GENERATE_TOKENS_KEYFILE_XXX)"
echo "foobar123" > "$keylocation"
zfs create -o encryption=on -o keylocation="file:///$keylocation" -o keyformat=passphrase "$SRC_DATASET"
rm "$keylocation"

SRC_MOUNT="$(zfs get -H -o value mountpoint "$SRC_DATASET")"
test -d "$SRC_MOUNT"

A="$SRC_DATASET"@a
B="$SRC_DATASET"@b
dd if=/dev/urandom of="$SRC_MOUNT"/dummy_data bs=1M count=5
zfs snapshot "$A"
dd if=/dev/urandom of="$SRC_MOUNT"/dummy_data bs=1M count=5
zfs snapshot "$B"

zfs create "$DST_ROOT"

cutoff="dd bs=1024 count=3K"

set +e
zfs send    "$A"         | $cutoff | zfs recv -s "$DST_ROOT"/full

zfs send "$A" | zfs recv "$DST_ROOT"/inc
zfs send    -i "$A" "$B" | $cutoff | zfs recv -s "$DST_ROOT"/inc

zfs send -w "$A"         | $cutoff | zfs recv -s "$DST_ROOT"/full_raw

zfs send -w "$A" | zfs recv "$DST_ROOT"/inc_raw
zfs send -w -i "$A" "$B" | $cutoff | zfs recv -s "$DST_ROOT"/inc_raw
set -e

TOKENS="$(zfs list -H -o name,receive_resume_token -r "$DST_ROOT")"

echo "$TOKENS" | awk -e '//{ if ($2 == "-") {  } else { system("zfs send -nvt " + $2);} }'

zpool destroy "$POOLNAME"
rm "$POOLIMG"