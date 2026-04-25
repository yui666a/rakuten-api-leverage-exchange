# バックテスト用 CSV データ作成手順

バックテスト実行 (`POST /api/v1/backtest/run`) で使う CSV の作成手順。

## 0. 自動更新 (推奨)

通常は **`csv-updater` コンテナ**が `backend/data/candles_*.csv` を毎時 5 分に自動で増分更新する。
ホストの `backend/data/` を bind mount しているので、コンテナ側の更新がそのままバックテストに反映される。

```bash
# 起動状況
docker compose ps csv-updater

# ログ
docker compose logs -f csv-updater

# 手動で即時更新
docker compose exec csv-updater /app/backend/scripts/fetch_incremental.sh
```

cron 式は `backend/Dockerfile.csv-updater` 内 (`5 * * * *`)。対象シンボルと足は
`backend/scripts/fetch_incremental.sh` の `SYMBOL_PAIRS` / `INTERVALS` を編集する。

以降の手順 (1 〜 8) は **新しいシンボル/足を追加する初回**、または **任意の期間を一括取得し直したい**ときの参考手順。

## 1. 前提

- 作業ディレクトリ: リポジトリルート
- 出力先（ホスト）: `backend/data/`
- API 実行時に参照するパス: `data/candles_XXX_YYY.csv`
  - 例: `data/candles_LTC_JPY_PT15M.csv`
- `csv-updater` は **CSV が既に存在する (symbol, interval) ペアにのみ追記**する。新しい組み合わせを追加するときは下記のページング取得スクリプトで初回ファイルを作ってから cron に任せる。

## 2. まずシンボル ID を確認する

```bash
curl -s 'https://exchange.rakuten-wallet.co.jp/api/v1/cfd/symbol' \
  | jq -r '.[] | "\(.currencyPair)\t\(.id)"'
```

例:
- `BTC_JPY` -> `7`
- `LTC_JPY` -> `10`

## 3. 最新付近だけ欲しい場合（簡易）

`cmd/backtest download` は簡易取得用。短い期間の確認にはこれで十分。

```bash
cd backend
go run ./cmd/backtest download --symbol LTC_JPY --interval PT15M --from 2026-01-01
go run ./cmd/backtest download --symbol LTC_JPY --interval PT1H  --from 2026-01-01
```

## 4. 全期間を取得したい場合（推奨）

楽天 API の `candlestick` は 1 回で最大 500 本返るため、`dateTo` を過去方向へずらしてページングする。

以下は `LTC_JPY` の `PT15M` と `PT1H` を作る例（必要に応じて `SYMBOL` / `SYMBOL_ID` を変更）。

```bash
cat > /tmp/fetch_ltc_history.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

BASE_URL="https://exchange.rakuten-wallet.co.jp/api/v1/candlestick"
SYMBOL="LTC_JPY"
SYMBOL_ID="10"
FROM_TS="1483228800000" # 2017-01-01 00:00:00 JST
OUT_DIR="backend/data"

mkdir -p "$OUT_DIR"

fetch_interval() {
  local interval="$1"
  local out_path="$OUT_DIR/candles_${SYMBOL}_${interval}.csv"
  local tmp_raw
  tmp_raw="$(mktemp)"

  local now_ms
  now_ms="$(($(date +%s) * 1000))"
  local to_ts="$now_ms"

  while true; do
    local url="${BASE_URL}?symbolId=${SYMBOL_ID}&candlestickType=${interval}&dateFrom=${FROM_TS}&dateTo=${to_ts}"
    local json
    json="$(curl -s "$url")"

    local len
    len="$(jq '.candlesticks | length' <<<"$json")"
    if [[ "$len" == "0" ]]; then
      break
    fi

    jq -r --arg symbol "$SYMBOL" --arg sid "$SYMBOL_ID" --arg interval "$interval" '
      .candlesticks[]
      | [$symbol, $sid, $interval, (.time|tostring), (.open|tostring), (.high|tostring), (.low|tostring), (.close|tostring), (.volume|tostring)]
      | @csv
    ' <<<"$json" >> "$tmp_raw"

    local min_ts
    min_ts="$(jq '[.candlesticks[].time] | min' <<<"$json")"

    if (( len < 500 )) || (( min_ts <= FROM_TS )); then
      break
    fi

    to_ts=$((min_ts - 1))
    sleep 0.25
  done

  {
    echo "symbol,symbol_id,interval,time,open,high,low,close,volume"
    sort -t, -k4,4n "$tmp_raw" | awk -F, '!seen[$4]++'
  } > "$out_path"

  rm -f "$tmp_raw"
  echo "[${interval}] done -> ${out_path}"
}

fetch_interval "PT15M"
fetch_interval "PT1H"
EOF

chmod +x /tmp/fetch_ltc_history.sh
/tmp/fetch_ltc_history.sh
```

## 5. 並び順チェック（時刻逆行がないか）

```bash
awk -F, 'NR==1{next} {gsub(/"/,"",$4); t=$4+0; if (NR>2 && t<prev) dec++; prev=t} END{print "rows=",NR-1,"decreases=",dec+0}' \
  backend/data/candles_LTC_JPY_PT15M.csv
```

- `decreases=0` なら時刻列は昇順で問題なし。

## 6. コンテナへ反映

`compose.yaml` の backend は `./backend/data` を bind mount しているので、ホストでファイルを更新するだけで即コンテナに反映される。コピー操作は不要。

確認:

```bash
docker compose exec backend ls -lh /app/backend/data | rg 'candles_LTC_JPY'
```

## 7. API で期間が見えるか確認

```bash
curl -sS 'http://localhost:38080/api/v1/backtest/csv-meta?data=data/candles_LTC_JPY_PT15M.csv' | jq
```

`fromTimestamp` / `toTimestamp` / `rowCount` が返れば利用可能。

## 8. バックテスト実行例

```bash
curl -sS -X POST 'http://localhost:38080/api/v1/backtest/run' \
  -H 'Content-Type: application/json' \
  -d '{
    "data":"data/candles_LTC_JPY_PT15M.csv",
    "dataHtf":"data/candles_LTC_JPY_PT1H.csv",
    "from":"2024-01-01",
    "to":"2024-12-31",
    "initialBalance":100000,
    "tradeAmount":0.01
  }' | jq '{id:.id, symbol:.config.symbol, totalTrades:.summary.totalTrades, totalReturn:.summary.totalReturn}'
```

