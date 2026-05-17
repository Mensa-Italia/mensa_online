#!/usr/bin/env bash
# Test end-to-end del flusso Zitadel + bearer su PB.
# Lancia: bash /tmp/test_zitadel_flow.sh
# Chiede HOST, EMAIL, PASSWORD a runtime (la password e` nascosta).

set -u

c_red=$'\033[31m'; c_grn=$'\033[32m'; c_ylw=$'\033[33m'; c_dim=$'\033[2m'; c_b=$'\033[1m'; c_off=$'\033[0m'
ok()   { echo "  ${c_grn}OK${c_off} $*"; }
ko()   { echo "  ${c_red}FAIL${c_off} $*"; }
warn() { echo "  ${c_ylw}WARN${c_off} $*"; }
hdr()  { echo; echo "${c_b}=== $* ===${c_off}"; }

pass_summary=0; fail_summary=0
mark_ok()   { pass_summary=$((pass_summary+1)); ok "$@"; }
mark_ko()   { fail_summary=$((fail_summary+1)); ko "$@"; }

http_get() {
  local label="$1" url="$2"
  local out status t
  out=$(curl -sS -o /dev/null -w "%{http_code} %{time_total}" \
        -H "Authorization: Bearer $AT" --max-time 20 "$url" 2>&1)
  status=${out%% *}
  t=${out##* }
  printf "  %-46s %sms  HTTP %s\n" "$label" "$(awk -v t=$t 'BEGIN{printf "%5d", t*1000}')" "$status"
  case "$status" in
    2*) pass_summary=$((pass_summary+1)) ;;
    *)  fail_summary=$((fail_summary+1)) ;;
  esac
}

read -p "Host svc (default https://svc.mensa.it): " HOST
HOST=${HOST:-https://svc.mensa.it}
HOST=${HOST%/}

read -p "Email: " EMAIL
[ -z "$EMAIL" ] && { ko "email vuota"; exit 1; }

read -s -p "Password: " PASS; echo
[ -z "$PASS" ] && { ko "password vuota"; exit 1; }

ZITADEL_HOST="https://auth.mensa.it"
CLIENT_ID="373315063999707142"

hdr "STEP 1 - login via $HOST/api/cs/auth-with-zitadel"
LOGIN=$(curl -sS -X POST "$HOST/api/cs/auth-with-zitadel" \
  -F email="$EMAIL" -F password="$PASS" --max-time 30 -w "\n---HTTP:%{http_code}")
HTTP1=$(echo "$LOGIN" | tail -1 | cut -d: -f2)
BODY1=$(echo "$LOGIN" | sed '$d')
if [ "$HTTP1" != "200" ]; then
  mark_ko "login fallito ($HTTP1)"
  echo "$BODY1" | head -c 500; echo
  unset PASS; exit 1
fi
AT=$(echo "$BODY1" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['access_token'])")
RT=$(echo "$BODY1" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['refresh_token'])")
ID=$(echo "$BODY1" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('id_token',''))")
EXP=$(echo "$BODY1" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('expires_in',0))")
PB_RECORD_ID=$(echo "$BODY1" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('record',{}).get('id',''))")
mark_ok "login 200 - access ${#AT}B refresh ${#RT}B id ${#ID}B exp=${EXP}s pb_id=$PB_RECORD_ID"

SUB=$(echo "$AT" | cut -d. -f2 | python3 -c "import sys,base64,json;s=sys.stdin.read().strip();s+='='*(-len(s)%4);print(json.loads(base64.urlsafe_b64decode(s)).get('sub',''))")
echo "  ${c_dim}sub=$SUB${c_off}"
unset PASS

hdr "STEP 2 - refresh diretto contro Zitadel (no client_secret)"
R1=$(curl -sS -X POST "$ZITADEL_HOST/oauth/v2/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "grant_type=refresh_token" \
  --data-urlencode "refresh_token=$RT" \
  --data-urlencode "client_id=$CLIENT_ID" \
  --max-time 20 -w "\n---HTTP:%{http_code}")
HTTP2=$(echo "$R1" | tail -1 | cut -d: -f2)
BODY2=$(echo "$R1" | sed '$d')
if [ "$HTTP2" != "200" ]; then
  mark_ko "refresh no-secret fallito ($HTTP2)"
  echo "$BODY2" | head -c 400; echo
  echo "  -> client su Zitadel forse non ancora Authentication Method = None"
else
  AT2=$(echo "$BODY2" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['access_token'])")
  RT2=$(echo "$BODY2" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('refresh_token',''))")
  mark_ok "refresh 200 - nuovo access ${#AT2}B refresh ${#RT2}B"
  if [ -z "$RT2" ] || [ "$RT" = "$RT2" ]; then
    warn "refresh_token NON ruotato (rotation off?)"
  else
    ok "refresh_token ruotato"
    AT="$AT2"
  fi
fi

hdr "STEP 3 - vecchio refresh deve essere morto"
R2=$(curl -sS -X POST "$ZITADEL_HOST/oauth/v2/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "grant_type=refresh_token" \
  --data-urlencode "refresh_token=$RT" \
  --data-urlencode "client_id=$CLIENT_ID" \
  --max-time 20 -w "\n---HTTP:%{http_code}")
HTTP3=$(echo "$R2" | tail -1 | cut -d: -f2)
if [ "$HTTP3" = "200" ]; then
  warn "vecchio refresh funziona ancora -> rotation NON attiva"
else
  mark_ok "vecchio refresh rifiutato ($HTTP3) - rotation OK"
fi

hdr "STEP 4 - batteria GET su PB con bearer Zitadel"
printf "  ${c_dim}%-46s %s  %s${c_off}\n" "endpoint" "  ms" "status"

for col in users members_registry events local_offices local_offices_admins \
           local_offices_links local_offices_test_dates local_offices_test_assistants \
           podcasts podcast_episodes podcast_episodes_transcript \
           quid_issues quid_articles quid_articles_audio \
           org_chart_groups org_chart_members chart configs \
           documents stamp sigs tickets boutique boutique_orders \
           calendar_link addons_private_keys user_zitadel_auth \
           user_notifications users_devices users_secrets; do
  http_get "list $col?perPage=1" "$HOST/api/collections/$col/records?perPage=1"
done

if [ -n "$PB_RECORD_ID" ]; then
  http_get "view users/$PB_RECORD_ID" "$HOST/api/collections/users/records/$PB_RECORD_ID"
fi

http_get "GET /api/cs/me (own record via bearer)" "$HOST/api/cs/me"

out=$(curl -sS -o /dev/null -w "%{http_code} %{time_total}" \
      -X POST -H "Authorization: Bearer $AT" --max-time 20 \
      "$HOST/api/collections/users/auth-refresh" 2>&1)
status=${out%% *}; t=${out##* }
printf "  %-46s %sms  HTTP %s\n" "POST users/auth-refresh" "$(awk -v t=$t 'BEGIN{printf "%5d", t*1000}')" "$status"
case "$status" in 2*) pass_summary=$((pass_summary+1)) ;; *) fail_summary=$((fail_summary+1)) ;; esac

http_get "health (no auth)" "$HOST/api/health"
http_get "members_registry filter is_active=true" "$HOST/api/collections/members_registry/records?perPage=3&filter=is_active%3Dtrue"
http_get "members_registry sort -created" "$HOST/api/collections/members_registry/records?perPage=3&sort=-created"
http_get "members_registry filter area=Lombardia" "$HOST/api/collections/members_registry/records?perPage=3&filter=area%3D%22Lombardia%22"
http_get "events sort -created" "$HOST/api/collections/events/records?perPage=3&sort=-created"
http_get "podcast_episodes sort -created" "$HOST/api/collections/podcast_episodes/records?perPage=3&sort=-created"
http_get "quid_articles sort -created" "$HOST/api/collections/quid_articles/records?perPage=3&sort=-created"

ui=$(curl -sS -o /dev/null -w "%{http_code} %{time_total}" \
     -H "Authorization: Bearer $AT" --max-time 10 "$ZITADEL_HOST/oidc/v1/userinfo" 2>&1)
status=${ui%% *}; t=${ui##* }
printf "  %-46s %sms  HTTP %s\n" "Zitadel /oidc/v1/userinfo" "$(awk -v t=$t 'BEGIN{printf "%5d", t*1000}')" "$status"
case "$status" in 2*) pass_summary=$((pass_summary+1)) ;; *) fail_summary=$((fail_summary+1)) ;; esac

hdr "STEP 5 - bearer rotto deve essere ignorato"
out=$(curl -sS -o /dev/null -w "%{http_code}" \
      -H "Authorization: Bearer eyJabc.def.ghi" --max-time 10 \
      "$HOST/api/collections/users/records/${PB_RECORD_ID:-anyid}" 2>&1)
if [ "$out" = "200" ]; then
  mark_ko "token rotto accettato ($out) - BUG"
else
  mark_ok "token rotto rifiutato (HTTP $out)"
fi

hdr "Riepilogo"
echo "  passed: ${c_grn}${pass_summary}${c_off}    failed: ${c_red}${fail_summary}${c_off}"
[ $fail_summary -eq 0 ] && echo "${c_grn}Tutto ok.${c_off}" || echo "${c_ylw}Qualcosa non torna, vedi sopra.${c_off}"
