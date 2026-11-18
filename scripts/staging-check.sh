#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

for key in STAGING_POSTGRES_PASSWORD STAGING_JWT_SECRET STAGING_INTERNAL_API_KEY STAGING_CORS_ALLOWED_ORIGINS; do
  [[ -n "${!key:-}" ]] || fail "$key harus diisi melalui environment staging, bukan file .env development."
done

[[ "$STAGING_POSTGRES_PASSWORD" =~ ^[A-Za-z0-9_-]{32,}$ ]] || fail 'STAGING_POSTGRES_PASSWORD harus acak, minimal 32 karakter URL-safe (huruf, angka, _ atau -).'
for key in STAGING_JWT_SECRET STAGING_INTERNAL_API_KEY; do
  [[ "${!key}" =~ ^[A-Za-z0-9_+/=-]{32,}$ ]] || fail "$key harus acak, minimal 32 karakter hex/base64/URL-safe tanpa whitespace."
done
[[ "$STAGING_JWT_SECRET" != "$STAGING_INTERNAL_API_KEY" && "$STAGING_JWT_SECRET" != "$STAGING_POSTGRES_PASSWORD" && "$STAGING_INTERNAL_API_KEY" != "$STAGING_POSTGRES_PASSWORD" ]] || fail 'Gunakan secret berbeda untuk database, JWT, dan API internal.'

[[ "$STAGING_CORS_ALLOWED_ORIGINS" != ,* && "$STAGING_CORS_ALLOWED_ORIGINS" != *, && "$STAGING_CORS_ALLOWED_ORIGINS" != *,,* ]] || fail 'Daftar origin staging tidak boleh memiliki entri kosong.'
[[ "$STAGING_CORS_ALLOWED_ORIGINS" != *[[:space:]]* ]] || fail 'Daftar origin staging tidak boleh mengandung whitespace.'
IFS=',' read -ra origins <<< "$STAGING_CORS_ALLOWED_ORIGINS"
for origin in "${origins[@]}"; do
  [[ "$origin" =~ ^https://[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+(:[0-9]{1,5})?$ ]] || fail 'STAGING_CORS_ALLOWED_ORIGINS harus berupa origin HTTPS eksplisit, dipisahkan koma tanpa spasi, path, atau wildcard.'
done

port="${STAGING_GATEWAY_PORT:-18080}"
[[ "$port" =~ ^[0-9]{4,5}$ ]] || fail 'STAGING_GATEWAY_PORT harus berupa port 1024–65535.'
(( 10#$port >= 1024 && 10#$port <= 65535 )) || fail 'STAGING_GATEWAY_PORT harus berupa port 1024–65535.'

if [[ -n "${STAGING_RESEND_API_KEY:-}" || -n "${STAGING_MAIL_FROM:-}" ]]; then
  [[ -n "${STAGING_RESEND_API_KEY:-}" && -n "${STAGING_MAIL_FROM:-}" ]] || fail 'Isi STAGING_RESEND_API_KEY dan STAGING_MAIL_FROM bersama-sama.'
else
  printf '%s\n' 'Catatan: pengiriman email/reset kata sandi belum diaktifkan.'
fi
if [[ -z "${STAGING_FIREBASE_PROJECT_ID:-}" ]]; then
  printf '%s\n' 'Catatan: login Firebase belum diaktifkan.'
fi

printf '%s\n' 'Validasi environment staging lulus; nilai secret tidak ditampilkan.'
