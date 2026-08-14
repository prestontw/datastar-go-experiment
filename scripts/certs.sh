#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

if ! command -v mkcert >/dev/null 2>&1; then
  cat >&2 <<'EOF'
mkcert is required to generate and trust the shared development certificate.

macOS:
  brew install mkcert
  # For Firefox support, also: brew install nss

Linux development uses the mkcert supplied by the Nix shell.
EOF
  exit 1
fi
if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to validate the development certificate." >&2
  exit 1
fi

mkdir -p .certs
mkcert -install

cert=.certs/localhost.pem
key=.certs/localhost-key.pem
root_ca="$(mkcert -CAROOT)/rootCA.pem"
regenerate=false

if [ ! -s "$cert" ] || [ ! -s "$key" ]; then
  regenerate=true
elif ! openssl verify -CAfile "$root_ca" "$cert" >/dev/null 2>&1; then
  echo "Replacing the localhost certificate: it was not issued by this host's mkcert CA."
  regenerate=true
else
  cert_public_key="$(openssl x509 -in "$cert" -pubkey -noout 2>/dev/null || true)"
  key_public_key="$(openssl pkey -in "$key" -pubout 2>/dev/null || true)"
  if [ -z "$cert_public_key" ] || [ "$cert_public_key" != "$key_public_key" ]; then
    echo "Replacing the localhost certificate: its private key does not match."
    regenerate=true
  fi
fi

if [ "$regenerate" = true ]; then
  rm -f "$cert" "$key"
  mkcert -cert-file "$cert" -key-file "$key" localhost 127.0.0.1 ::1
fi

chmod 0644 "$cert"
chmod 0600 "$key"
