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

mkdir -p .certs
mkcert -install

cert=.certs/localhost.pem
key=.certs/localhost-key.pem
if [ ! -s "$cert" ] || [ ! -s "$key" ]; then
  rm -f "$cert" "$key"
  mkcert -cert-file "$cert" -key-file "$key" localhost 127.0.0.1 ::1
fi

chmod 0644 "$cert"
chmod 0600 "$key"
