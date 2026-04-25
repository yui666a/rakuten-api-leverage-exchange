#!/usr/bin/env bash
# Incrementally append latest candlestick data to the CSV files used by the
# backtest engine. Designed to run inside the csv-updater container, which
# bind-mounts ./backend/data so the host filesystem stays the source of truth.
#
# For each (symbol, interval) we read max(time) from the existing CSV, page
# forward via Rakuten's candlestick API (500 bars per call), then append +
# dedupe + sort by numeric time. The leading-quote-aware sort key avoids the
# `sort -u` collapse bug that earlier truncated files to one row.

set -euo pipefail

BASE_URL="${RAKUTEN_API_BASE:-https://exchange.rakuten-wallet.co.jp/api/v1/candlestick}"
OUT_DIR="${CSV_OUT_DIR:-/app/backend/data}"

SYMBOL_PAIRS=(
  "BTC_JPY:7"
  "ETH_JPY:8"
  "BCH_JPY:9"
  "LTC_JPY:10"
  "XRP_JPY:11"
  "ADA_JPY:14"
  "DOT_JPY:15"
  "XLM_JPY:16"
)

INTERVALS="PT1M PT5M PT15M PT1H P1D P1W"

log() { printf '[csv-updater %s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

now_ms() { echo "$(($(date +%s) * 1000))"; }

csv_max_time() {
  local file="$1"
  if [[ ! -s "$file" ]]; then
    echo 0
    return
  fi
  awk -F, 'NR==1{next}{gsub(/"/,"",$4); t=$4+0; if(t>m) m=t} END{print m+0}' "$file"
}

fetch_one() {
  local symbol="$1" sid="$2" interval="$3"
  local out="$OUT_DIR/candles_${symbol}_${interval}.csv"

  if [[ ! -f "$out" ]]; then
    log "skip $symbol $interval: file not found ($out)"
    return
  fi

  local from_ms
  from_ms="$(csv_max_time "$out")"
  if [[ "$from_ms" -le 0 ]]; then
    log "skip $symbol $interval: cannot determine start"
    return
  fi
  from_ms=$((from_ms + 1))

  local target_ms cur_to pages fetched
  target_ms="$(now_ms)"
  cur_to="$target_ms"
  pages=0
  fetched=0

  if (( from_ms >= target_ms )); then
    log "ok   $symbol $interval: already up to date"
    return
  fi

  local tmp_new
  tmp_new="$(mktemp)"

  while true; do
    local url="${BASE_URL}?symbolId=${sid}&candlestickType=${interval}&dateFrom=${from_ms}&dateTo=${cur_to}"
    local json
    json="$(curl -sS "$url")" || { log "err  $symbol $interval: curl failed"; break; }

    local len
    len="$(jq '.candlesticks | length' <<<"$json" 2>/dev/null || echo 0)"
    if [[ -z "$len" || "$len" == "null" || "$len" == "0" ]]; then
      break
    fi

    jq -r --arg symbol "$symbol" --arg sid "$sid" --arg interval "$interval" '
      .candlesticks[]
      | [$symbol, $sid, $interval, (.time|tostring), (.open|tostring), (.high|tostring), (.low|tostring), (.close|tostring), (.volume|tostring)]
      | @csv
    ' <<<"$json" >>"$tmp_new"

    fetched=$((fetched + len))
    pages=$((pages + 1))

    local min_ts
    min_ts="$(jq '[.candlesticks[].time] | min' <<<"$json")"

    if (( len < 500 )); then break; fi
    if (( min_ts <= from_ms )); then break; fi
    cur_to=$((min_ts - 1))
    sleep 0.15
  done

  if [[ ! -s "$tmp_new" ]]; then
    log "ok   $symbol $interval: no new bars"
    rm -f "$tmp_new"
    return
  fi

  # Merge: existing data minus header + new rows, dedupe by numeric time, sort.
  local tmp_merged
  tmp_merged="$(mktemp)"
  {
    tail -n +2 "$out"
    cat "$tmp_new"
  } | awk -F, '{ key=$4; gsub(/"/,"",key); printf "%s\t%s\n", key+0, $0 }' \
    | sort -t$'\t' -k1,1n -u \
    | cut -f2- >"$tmp_merged"

  {
    echo "symbol,symbol_id,interval,time,open,high,low,close,volume"
    cat "$tmp_merged"
  } >"$out"

  rm -f "$tmp_new" "$tmp_merged"

  local new_max_iso new_max_ms
  new_max_ms="$(csv_max_time "$out")"
  new_max_iso="$(date -u -d "@$((new_max_ms / 1000))" '+%Y-%m-%d %H:%M:%S UTC' 2>/dev/null || echo "$new_max_ms")"
  log "ok   $symbol $interval: +${fetched} rows (pages=${pages}) latest=${new_max_iso}"
}

log "begin csv update OUT_DIR=$OUT_DIR"

for pair in "${SYMBOL_PAIRS[@]}"; do
  symbol="${pair%%:*}"
  sid="${pair#*:}"
  for interval in $INTERVALS; do
    fetch_one "$symbol" "$sid" "$interval"
  done
done

log "done"
