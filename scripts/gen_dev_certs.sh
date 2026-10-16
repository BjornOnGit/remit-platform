#!/usr/bin/env bash
# Generates a dev CA, a ledger server certificate and an api-gateway client certificate
# into ./certs (gitignored). Re-running replaces them.
set -euo pipefail

OUT="$(cd "$(dirname "$0")/.." && pwd)/certs"
mkdir -p "$OUT"
cd "$OUT"

openssl genrsa -out ca.key 2048
openssl req -x509 -new -nodes -key ca.key -sha256 -days 365 -subj "/CN=remit-dev-ca" -out ca.crt

# Ledger (server). SANs cover local runs and the docker-compose service name.
openssl genrsa -out ledger.key 2048
openssl req -new -key ledger.key -subj "/CN=ledger" -out ledger.csr
printf "subjectAltName=DNS:localhost,DNS:ledger,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n" > ledger.ext
openssl x509 -req -in ledger.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out ledger.crt -days 365 -sha256 -extfile ledger.ext

# API gateway (client)
openssl genrsa -out gateway.key 2048
openssl req -new -key gateway.key -subj "/CN=api-gateway" -out gateway.csr
printf "extendedKeyUsage=clientAuth\n" > gateway.ext
openssl x509 -req -in gateway.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out gateway.crt -days 365 -sha256 -extfile gateway.ext

rm -f ./*.csr ./*.ext ./*.srl
echo "certs written to $OUT"