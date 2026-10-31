#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SIGN_ID="${SIGN_ID:-MyAgent Self-Signed}"
KEYCHAIN="${SIGNING_KEYCHAIN:-$HOME/Library/Keychains/login.keychain-db}"
DAYS="${SIGN_DAYS:-3650}"
OUT_DIR="${SIGN_OUT_DIR:-$ROOT/dist/signing}"
P12_NAME="${SIGN_P12_NAME:-MyAgent-SelfSigned.p12}"
TRUST_SYSTEM="${SIGN_TRUST_SYSTEM:-0}"
KEYCHAIN_PASSWORD="${SIGN_KEYCHAIN_PASSWORD:-}"

usage() {
  cat <<EOF
Usage:
  $0 create                 Create a self-signed code signing identity and export a .p12
  $0 import <file.p12>      Import an existing identity on another Mac
  $0 status                 Show the local signing identity and the app requirement

Environment:
  SIGN_ID                   Identity name (default: $SIGN_ID)
  SIGNING_KEYCHAIN          Target keychain (default: ~/Library/Keychains/login.keychain-db)
  SIGN_DAYS                 Certificate validity in days (default: $DAYS)
  SIGN_OUT_DIR              Where to write the .p12 (default: dist/signing)
  SIGN_P12_PASSWORD         Password for the .p12 (default: generated on create, prompted on import)
  SIGN_TRUST_SYSTEM         Set to 1 to trust the certificate in the System keychain with sudo (CI)
  SIGN_KEYCHAIN_PASSWORD    Keychain password for set-key-partition-list (CI)
EOF
}

run_with_timeout() {
  local seconds="$1"
  shift
  /usr/bin/perl -e 'alarm shift; exec @ARGV' "$seconds" "$@"
}

trust_certificate() {
  local cert="$1"
  if [ "$TRUST_SYSTEM" = "1" ]; then
    echo "→ trusting certificate in the system keychain"
    if ! run_with_timeout 30 sudo security add-trusted-cert -d -r trustRoot \
      -p codeSign -k /Library/Keychains/System.keychain "$cert"; then
      echo "error: could not trust the certificate in the system keychain." >&2
      exit 1
    fi
  else
    echo "→ trusting certificate for code signing"
    if ! run_with_timeout 15 security add-trusted-cert -r trustRoot \
      -p codeSign -k "$KEYCHAIN" "$cert"; then
      echo "error: could not trust the certificate." >&2
      echo "Open Keychain Access, select the certificate, and set 'Always Trust'" >&2
      echo "for Code Signing." >&2
      exit 1
    fi
  fi
}

allow_codesign_access() {
  if [ -n "$KEYCHAIN_PASSWORD" ]; then
    echo "→ allowing codesign access to the keychain"
    if ! run_with_timeout 15 security set-key-partition-list \
      -S apple-tool:,apple:,codesign: -s -k "$KEYCHAIN_PASSWORD" \
      "$KEYCHAIN" >/dev/null; then
      echo "error: could not update the keychain partition list." >&2
      exit 1
    fi
  fi
}

have_identity() {
  security find-identity -v -p codesigning 2>/dev/null \
    | grep -F "\"$SIGN_ID\"" >/dev/null
}

cleanup_tmp() {
  if [ -n "${TMP_DIR:-}" ]; then
    rm -rf "$TMP_DIR"
  fi
}

create_identity() {
  if have_identity; then
    echo "Signing identity already exists: $SIGN_ID"
    echo "If you need to copy it to another Mac, export the identity from Keychain Access."
    return 0
  fi

  mkdir -p "$OUT_DIR"
  chmod 700 "$OUT_DIR"
  TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/myagent-sign.XXXXXX")"
  trap cleanup_tmp EXIT

  cat > "$TMP_DIR/openssl.cnf" <<EOF
[req]
distinguished_name = dn
x509_extensions = ext
prompt = no

[dn]
CN = $SIGN_ID
O = MyAgent

[ext]
basicConstraints = critical, CA:true
keyUsage = critical, digitalSignature, keyCertSign
extendedKeyUsage = critical, codeSigning
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always
EOF

  local password="${SIGN_P12_PASSWORD:-}"
  if [ -z "$password" ]; then
    password="$(/usr/bin/openssl rand -hex 12)"
  fi

  /usr/bin/openssl req -x509 -newkey rsa:2048 -sha256 -days "$DAYS" -nodes \
    -keyout "$TMP_DIR/key.pem" -out "$TMP_DIR/cert.pem" \
    -config "$TMP_DIR/openssl.cnf" >/dev/null 2>&1

  # macOS security(1) cannot read modern PBES2/AES PKCS#12 files, so use the
  # legacy PBE algorithms explicitly.
  /usr/bin/openssl pkcs12 -export \
    -inkey "$TMP_DIR/key.pem" -in "$TMP_DIR/cert.pem" \
    -out "$TMP_DIR/identity.p12" -name "$SIGN_ID" \
    -passout "pass:$password" \
    -certpbe PBE-SHA1-3DES -keypbe PBE-SHA1-3DES -macalg sha1 >/dev/null 2>&1

  echo "→ importing identity into $KEYCHAIN"
  if ! run_with_timeout 30 security import "$TMP_DIR/identity.p12" \
    -k "$KEYCHAIN" -P "$password" -T /usr/bin/codesign >/dev/null; then
    echo "error: security import failed or timed out." >&2
    exit 1
  fi
  echo "→ imported identity"
  allow_codesign_access

  # Trust only for code signing. Gatekeeper still requires Developer ID +
  # notarization; this trust is what lets codesign use the identity locally.
  trust_certificate "$TMP_DIR/cert.pem"

  if ! have_identity; then
    echo "error: imported the certificate but identity is still not valid." >&2
    echo "Try opening Keychain Access, selecting the certificate, and setting" >&2
    echo "'Always Trust' for Code Signing." >&2
    exit 1
  fi

  cp "$TMP_DIR/identity.p12" "$OUT_DIR/$P12_NAME"
  chmod 600 "$OUT_DIR/$P12_NAME"

  cat <<EOF

✓ Created code signing identity: $SIGN_ID
  Validity: $DAYS days
  Exported: $OUT_DIR/$P12_NAME
  Password: $password

Next steps:
  1. Keep the .p12 and password somewhere safe (a password manager or encrypted disk).
  2. On the second Mac, copy the .p12 over and run:
       mise run sign-import -- /path/to/$P12_NAME
  3. Build normally:
       mise run app
     The build script picks this identity up automatically.
EOF
}

import_identity() {
  local p12="${1:-}"
  if [ -z "$p12" ] || [ ! -f "$p12" ]; then
    echo "error: pass the path to a .p12 file." >&2
    usage >&2
    exit 1
  fi

  if have_identity; then
    echo "Signing identity already exists: $SIGN_ID"
    return 0
  fi

  local password="${SIGN_P12_PASSWORD:-}"
  if [ -z "$password" ]; then
    read -rsp "Password for $(basename "$p12"): " password
    echo
  fi

  TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/myagent-sign.XXXXXX")"
  trap cleanup_tmp EXIT

  /usr/bin/openssl pkcs12 -in "$p12" -clcerts -nokeys \
    -passin "pass:$password" -out "$TMP_DIR/cert.pem" >/dev/null 2>&1

  echo "→ importing identity into $KEYCHAIN"
  if ! run_with_timeout 30 security import "$p12" \
    -k "$KEYCHAIN" -P "$password" -T /usr/bin/codesign >/dev/null; then
    echo "error: security import failed or timed out." >&2
    exit 1
  fi
  echo "→ imported identity"
  allow_codesign_access

  trust_certificate "$TMP_DIR/cert.pem"

  if ! have_identity; then
    echo "error: imported the certificate but identity is still not valid." >&2
    echo "Try opening Keychain Access, selecting the certificate, and setting" >&2
    echo "'Always Trust' for Code Signing." >&2
    exit 1
  fi

  echo "✓ Imported code signing identity: $SIGN_ID"
}

show_status() {
  if ! have_identity; then
    echo "No valid identity named \"$SIGN_ID\"." >&2
    echo "Run: mise run sign-setup" >&2
    exit 1
  fi

  security find-identity -v -p codesigning | grep -F "\"$SIGN_ID\""

  if [ -d "$ROOT/dist/MyAgent.app" ]; then
    echo
    codesign -d -r- "$ROOT/dist/MyAgent.app" 2>&1
  fi
}

case "${1:-}" in
  create|init)
    create_identity
    ;;
  import)
    shift
    import_identity "$@"
    ;;
  status)
    show_status
    ;;
  *)
    usage
    exit 1
    ;;
esac
