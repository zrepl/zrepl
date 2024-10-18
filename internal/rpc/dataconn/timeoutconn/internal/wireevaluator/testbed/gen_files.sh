#!/usr/bin/env bash
set -e

cd "$( dirname "${BASH_SOURCE[0]}")"

FILESDIR="$(pwd)"/files

echo "[INFO] compile binary"
pushd .. >/dev/null
go build -o $FILESDIR/wireevaluator
popd >/dev/null

if [ "$GENKEYS" == "" ]; then
    echo "[INFO] GENKEYS environment variable not set, assumed to be valid"
    exit 0
fi

echo "[INFO] gen ssh key"
ssh-keygen -f "$FILESDIR/wireevaluator.ssh_client_identity" -t ed25519

echo "[INFO] gen tls keys"

cakey="$FILESDIR/wireevaluator.tls.ca.key"
cacrt="$FILESDIR/wireevaluator.tls.ca.crt"
hostprefix="$FILESDIR/wireevaluator.tls"

openssl genrsa -out "$cakey" 4096
openssl req -x509 -new -nodes -key "$cakey" -sha256 -days 1 -out "$cacrt"

declare -a HOSTS
HOSTS+=("theserver")
HOSTS+=("theclient")

for host in "${HOSTS[@]}"; do
    key="${hostprefix}.${host}.key"
    csr="${hostprefix}.${host}.csr"
    crt="${hostprefix}.${host}.crt"
    openssl genrsa -out "$key" 2048

    (
        echo "."
        echo "."
        echo "."
        echo "."
        echo "."
        echo $host
        echo "."
        echo "."
        echo "."
        echo "."
    ) | openssl req -new -key "$key" -out "$csr"

    openssl x509 -req -in "$csr" -CA "$cacrt" -CAkey "$cakey" -CAcreateserial -out "$crt" -days 1 -sha256

done
