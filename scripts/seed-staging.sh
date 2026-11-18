#!/usr/bin/env bash
# Seed staging with demo organizers, published events, reviews and going rows.
# Usage on the staging host: bash scripts/seed-staging.sh
# Idempotent: existing accounts and event slugs are skipped, review/going seeds
# use ON CONFLICT DO NOTHING. Demo passwords are stored, never printed.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RUNTIME_CONF="${STAGING_RUNTIME_CONF:-$HOME/.config/fayrly-staging/runtime.conf}"
if [[ -f "$RUNTIME_CONF" ]]; then
  set -a
  # shellcheck disable=SC1090
  . "$RUNTIME_CONF"
  set +a
fi
: "${STAGING_POSTGRES_PASSWORD:?Set STAGING_POSTGRES_PASSWORD atau source runtime.conf staging}"

PROJECT="${STAGING_PROJECT_NAME:-fayrly-staging}"
BASE_URL="${BASE_URL:-http://127.0.0.1:${STAGING_GATEWAY_PORT:-18080}}"
DEMO_CONF="${STAGING_DEMO_CONF:-$HOME/.config/fayrly-staging/demo-accounts.conf}"
export BASE_URL

COMPOSE=(docker compose --env-file /dev/null --project-name "$PROJECT" -f compose.staging.yml)

psql() {
  "${COMPOSE[@]}" exec -T postgres psql -U fayrly -d "$1" -v ON_ERROR_STOP=1 -At -c "$2"
}

KARSA_EMAIL='organizer.karsa@fayrly.my.id'
HARAPAN_EMAIL='organizer.harapan@fayrly.my.id'
export KARSA_EMAIL HARAPAN_EMAIL

if [[ -f "$DEMO_CONF" ]]; then
  set -a
  # shellcheck disable=SC1090
  . "$DEMO_CONF"
  set +a
else
  mkdir -p "$(dirname "$DEMO_CONF")"
  (
    umask 077
    {
      printf 'export DEMO_KARSA_PASSWORD=%q\n' "$(openssl rand -hex 24)"
      printf 'export DEMO_HARAPAN_PASSWORD=%q\n' "$(openssl rand -hex 24)"
    } > "$DEMO_CONF"
  )
  set -a
  # shellcheck disable=SC1090
  . "$DEMO_CONF"
  set +a
  printf 'Kredensial akun demo disimpan di %s (chmod 600).\n' "$DEMO_CONF"
fi
: "${DEMO_KARSA_PASSWORD:?DEMO_KARSA_PASSWORD missing in $DEMO_CONF}"
: "${DEMO_HARAPAN_PASSWORD:?DEMO_HARAPAN_PASSWORD missing in $DEMO_CONF}"
export DEMO_KARSA_PASSWORD DEMO_HARAPAN_PASSWORD

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
STATUS=''

call() {
  local method=$1 path=$2 token=${3:-} data=${4:-}
  local args=(-sS --max-time 20 -o "$TMP_DIR/body.json" -w '%{http_code}' -X "$method" "$BASE_URL$path")
  if [[ -n "$token" ]]; then args+=(-H "Authorization: Bearer $token"); fi
  if [[ -n "$data" ]]; then args+=(-H 'Content-Type: application/json' --data-binary "$data"); fi
  STATUS=$(curl "${args[@]}") || STATUS='000'
}

field() {
  python3 - "$TMP_DIR/body.json" "$1" <<'PY'
import json, sys
value = json.load(open(sys.argv[1]))
for part in sys.argv[2].split("."):
    value = value[int(part)] if part.isdigit() else value[part]
print(value)
PY
}

printf '\n== Akun organizer demo ==\n'
register() {
  local email=$1 name=$2 password=$3
  call POST /api/v1/auth/register '' "{\"email\":\"$email\",\"name\":\"$name\",\"password\":\"$password\"}"
  case "$STATUS" in
    201) printf '  dibuat: %s\n' "$email" ;;
    409) printf '  sudah ada: %s\n' "$email" ;;
    *) printf 'register %s gagal (HTTP %s)\n' "$email" "$STATUS" >&2; exit 1 ;;
  esac
}
register "$KARSA_EMAIL" 'Karsa Live' "$DEMO_KARSA_PASSWORD"
register "$HARAPAN_EMAIL" 'Yayasan Harapan' "$DEMO_HARAPAN_PASSWORD"

printf '\n== Role manager (event:write + event:publish) ==\n'
psql fayrly_user "INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id FROM users u CROSS JOIN roles r
WHERE u.email IN ('$KARSA_EMAIL', '$HARAPAN_EMAIL') AND r.key = 'manager'
ON CONFLICT (user_id, role_id) DO NOTHING;"
printf '  role manager diberikan ke kedua akun demo\n'

printf '\n== Event demo (buat + publish lewat API publik) ==\n'
python3 - <<'PY'
import json
import os
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone

BASE = os.environ['BASE_URL']
ACCOUNTS = {
    'karsa': (os.environ['KARSA_EMAIL'], os.environ['DEMO_KARSA_PASSWORD'], 'Karsa Live'),
    'harapan': (os.environ['HARAPAN_EMAIL'], os.environ['DEMO_HARAPAN_PASSWORD'], 'Yayasan Harapan'),
}
COVER = 'https://images.unsplash.com/photo-{}?auto=format&fit=crop&w=900&q=80'


def api(method, path, token=None, body=None, expected=(200, 201)):
    headers = {}
    data = None
    if token:
        headers['Authorization'] = 'Bearer ' + token
    if body is not None:
        data = json.dumps(body).encode()
        headers['Content-Type'] = 'application/json'
    request = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        response = urllib.request.urlopen(request, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read()
        payload = json.loads(raw) if raw else None
        if response.status not in expected:
            raise SystemExit(f'{method} {path}: expected {expected}, got {response.status}: {raw[:400]!r}')
        return payload, response.status


tokens = {}
for key, (email, password, _) in ACCOUNTS.items():
    session, _ = api('POST', '/api/v1/auth/login', body={'email': email, 'password': password})
    tokens[key] = session['access_token']

existing, _ = api('GET', '/api/v1/events?status=published&page_size=100')
slugs = {event['slug'] for event in existing['data']}

now = datetime.now(timezone.utc)


def at(days, hour, minute=0):
    moment = (now + timedelta(days=days)).replace(hour=hour, minute=minute, second=0, microsecond=0)
    return moment.strftime('%Y-%m-%dT%H:%M:%SZ')


EVENTS = [
    dict(owner='karsa', slug='senja-musik-festival-2026', title='Senja Musik Festival 2026', type='concert',
         description='Festival musik sore sampai malam dengan tiga panggung: indie, pop, dan elektronik. Area kuliner dan merch resmi tersedia di lokasi.',
         venue_name='GBK Area Parkir Timur', venue_address='Jl. Pintu Satu Senayan', city='Jakarta',
         starts=at(17, 16), ends=at(17, 23), capacity=5000, price_min=150000, price_max=850000,
         cover='1506157786151-b8491531f063', tags=['musik', 'festival', 'outdoor']),
    dict(owner='karsa', slug='malam-akustik-kota-lama', title='Malam Akustik Kota Lama', type='concert',
         description='Konser akustik intim di kawasan Kota Lama. Lima penampil membawakan lagu-lagu daur ulang dengan aransemen akustik penuh.',
         venue_name='Kota Lama Entertainment Hall', venue_address='Jl. Letjen Suprapto', city='Semarang',
         starts=at(31, 19, 30), ends=at(31, 22), capacity=800, price_min=75000, price_max=250000,
         cover='1470229722913-7c0e2dbbafd3', tags=['akustik', 'intim']),
    dict(owner='karsa', slug='konser-tahun-baru-harmoni', title='Konser Tahun Baru Harmoni', type='concert',
         description='Tutup tahun dengan konser orkestra dan paduan suara gabungan, ditutup hitung mundur dan pesta kembang api di area venue.',
         venue_name='Istora Senayan', venue_address='Kompleks GBK', city='Jakarta',
         starts=at(43, 20), ends=at(44, 0, 30), capacity=3500, price_min=200000, price_max=1200000,
         cover='1514525253161-7a46d19cd819', tags=['tahun baru', 'konser']),
    dict(owner='karsa', slug='kopdar-kreatif-bandung', title='Kopdar Komunitas Kreatif Bandung', type='community',
         description='Kopdar bulanan pelaku industri kreatif: sesi berbagi, portofolio review, dan networking santai. Gratis dan terbuka untuk umum.',
         venue_name='Creative Hub Bandung', venue_address='Jl. Braga No. 12', city='Bandung',
         starts=at(10, 10), ends=at(10, 15), capacity=150, price_min=0, price_max=0,
         cover='1517457373958-b7bdd4587205', tags=['komunitas', 'kreatif', 'kopdar']),
    dict(owner='karsa', slug='festival-kuliner-nusantara', title='Festival Kuliner Nusantara', type='community',
         description='Bazar kuliner dari 30 kota dengan demo masak, lomba makan tradisional, dan panggung hiburan keluarga sepanjang hari.',
         venue_name='Alun-Alun Kidul', venue_address='Jl. Alun-Alun Kidul', city='Yogyakarta',
         starts=at(18, 11), ends=at(18, 21), capacity=2000, price_min=25000, price_max=100000,
         cover='1511632765486-a01980e01a18', tags=['kuliner', 'bazar', 'keluarga']),
    dict(owner='karsa', slug='workshop-bazar-daur-ulang', title='Workshop & Bazar Daur Ulang', type='community',
         description='Workshop mengolah sampah rumah tangga jadi produk bernilai, plus bazar tukar-barang preloved. Bawa botol minum sendiri ya.',
         venue_name='Balai Kota Depok', venue_address='Jl. Margonda Raya', city='Depok',
         starts=at(11, 9), ends=at(11, 16), capacity=200, price_min=0, price_max=0,
         cover='1488521787991-ed7bbaae773c', tags=['daur ulang', 'workshop', 'bazar']),
    dict(owner='harapan', slug='konser-amal-suara-harapan', title='Konser Amal Suara Harapan', type='charity',
         description='Konser amal tahunan Yayasan Harapan. Seluruh pendapatan tiket disalurkan untuk beasiswa anak prasejahtera di Jawa Barat.',
         venue_name='Grand Hall Trans Convention', venue_address='Jl. Gatot Subroto No. 289', city='Bandung',
         starts=at(24, 19), ends=at(24, 22, 30), capacity=1200, price_min=100000, price_max=300000,
         cover='1492684223066-81342ee5ff30', tags=['amal', 'konser'],
         beneficiary='Yayasan Harapan Anak', donation_target=150000000),
    dict(owner='harapan', slug='lari-amal-peduli-anak-5k', title='Lari Amal Peduli Anak 5K', type='charity',
         description='Fun run 5K menyusuri taman kota. Setiap kilometer yang diselesaikan peserta dikonversi jadi donasi buku untuk rumah belajar anak.',
         venue_name='Taman Bungkul', venue_address='Jl. Raya Darmo', city='Surabaya',
         starts=at(53, 6), ends=at(53, 10), capacity=3000, price_min=50000, price_max=150000,
         cover='1559027615-cd4628902d4a', tags=['lari', 'amal', 'olahraga'],
         beneficiary='Rumah Belajar Anak', donation_target=200000000),
    dict(owner='harapan', slug='gala-donasi-pendidikan', title='Gala Donasi Pendidikan', type='charity',
         description='Malam gala dan lelang karya seni untuk pendanaan beasiswa satu tahun penuh bagi 50 siswa berprestasi dari keluarga prasejahtera.',
         venue_name='Ballroom Hotel Indonesia', venue_address='Jl. M.H. Thamrin', city='Jakarta',
         starts=at(67, 18, 30), ends=at(67, 22), capacity=400, price_min=250000, price_max=1000000,
         cover='1593113598332-cd288d649433', tags=['gala', 'donasi', 'pendidikan'],
         beneficiary='Beasiswa Harapan', donation_target=500000000),
]

created = published = skipped = 0
for event in EVENTS:
    if event['slug'] in slugs:
        skipped += 1
        print(f"  lewati (sudah ada): {event['slug']}")
        continue
    email, _, organizer = ACCOUNTS[event['owner']]
    body = {
        'title': event['title'], 'slug': event['slug'], 'description': event['description'],
        'type': event['type'], 'venue_name': event['venue_name'], 'venue_address': event['venue_address'],
        'city': event['city'], 'country': 'ID', 'starts_at': event['starts'], 'ends_at': event['ends'],
        'capacity': event['capacity'], 'price_min': event['price_min'], 'price_max': event['price_max'],
        'currency': 'IDR', 'cover_image_url': COVER.format(event['cover']),
        'organizer_name': organizer, 'contact_email': email, 'tags': event['tags'],
    }
    if 'beneficiary' in event:
        body['beneficiary'] = event['beneficiary']
        body['donation_target'] = event['donation_target']
    payload, status = api('POST', '/api/v1/events', tokens[event['owner']], body, expected=(201, 409))
    if status == 409:
        skipped += 1
        print(f"  lewati (slug dipakai): {event['slug']}")
        continue
    event_id = payload['data']['id']
    api('POST', f'/api/v1/events/{event_id}/status', tokens[event['owner']], {'status': 'published'}, expected=(200, 409))
    created += 1
    published += 1
    print(f"  dibuat + published: {event['title']}")

print(f'  total: {created} dibuat, {published} published, {skipped} dilewati')
PY

printf '\n== Ulasan & "others are going" ==\n'
"${COMPOSE[@]}" exec -T postgres psql -U fayrly -d fayrly_event -v ON_ERROR_STOP=1 < scripts/seed_reviews_going.sql
printf '  seed ulasan/going selesai\n'

printf '\n== Ringkasan ==\n'
psql fayrly_event "SELECT 'event published: ' || count(*) FROM events WHERE status = 'published' AND deleted_at IS NULL;"
psql fayrly_event "SELECT 'baris going: ' || count(*) FROM event_going;"
psql fayrly_event "SELECT 'ulasan: ' || count(*) FROM event_reviews;"
call GET '/api/v1/events?status=published&page_size=100'
if [[ "$STATUS" != '200' ]]; then
  printf 'API publik gagal (HTTP %s)\n' "$STATUS" >&2
  exit 1
fi
printf 'event terlihat di API publik: %s\n' "$(field pagination.total)"
printf '\nSelesai. Kredensial demo: %s\n' "$DEMO_CONF"