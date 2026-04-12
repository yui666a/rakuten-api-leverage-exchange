# 楽天ウォレット証拠金取引 API エラーコードリファレンス

楽天ウォレットの証拠金取引 API ([公式ドキュメント](https://www.rakuten-wallet.co.jp/service/api-leverage-exchange/)) が返すエラーコードを本システムの扱いと合わせて整理したもの。

- **対象 API**: `https://exchange.rakuten-wallet.co.jp`
- **更新**: 2026-04-12 時点の公式ページをベースに記載
- **一次情報**: 公式ページが正。齟齬があれば公式ページを信じること。本ドキュメントは運用者の手元リファレンスで、バージョン追従は手動。

## レート制限

楽天 Private API は **ユーザーごとに 1 リクエストあたり 200 ミリ秒以上の間隔** を開けることを要求している (公式記載: "APIにはユーザー毎に前回のリクエストから200mSec間隔を開ける制限があります")。

- 違反時のエラーコード: **`20010` / `AUTHENTICATION_ERROR_TOO_MANY_REQUESTS`**
- 本システムでは `backend/internal/infrastructure/rakuten/rest_client.go` の `waitForRateLimit` が 220ms マージンでクライアント側直列化を担保する。
- それでもサーバー側の受信時刻ジッタで稀に 20010 を踏むことがあるため、残高/ポジション同期は `backend/cmd/retry.go` の `retryOn20010` で 20010 のみ最大 3 回リトライする (backoff 300ms → 600ms → 1200ms)。
- 発注系 (`CreateOrder`) では 220ms スロットラーが構造的に直列化を保証する前提で、20010 リトライは入れていない (参考: `backend/internal/interfaces/api/handler/trade.go` の `GetAllTrades` コメント)。

## HTTP ステータスコード

公式は「通信成否は全てレスポンスの HTTP ステータスコードで判別」と明記している。本システムの観測では、API エラー本文 (`{"code":XXXXX}`) は **HTTP 500 に乗って返ってくる** ケースがある (例: 20010 も HTTP 500 で降ってくる)。したがって 500 を受けた場合はボディの `code` を見てエラー種別を判定する必要がある。

## 10000 番台: 共通エラー

| コード | 名称 | 意味 | 本システムの扱い |
|---|---|---|---|
| 10000 | `COMMON_ERROR_NOT_FOUND` | リソースが見つからない | 呼び出し側で警告ログのみ。リトライしない。 |
| 10001 | `COMMON_ERROR_SYSTEM_ERROR` | 楽天側のシステムエラー | リトライしない。頻発する場合は楽天側の障害を疑う。 |
| 10005 | `COMMON_ERROR_IN_MAINTENANCE` | メンテナンス中 | リトライしても意味がないので warn のみ。メンテ時間帯は公式のお知らせを参照。 |
| 10006 | `COMMON_ERROR_IN_DAILY_MAINTENANCE` | 日次メンテナンス中 | 日次メンテ中は同期・発注とも諦める。復旧後は自動で再同期ループが追いつく。 |

## 20000 番台: 認証エラー

| コード | 名称 | 意味 | 本システムの扱い |
|---|---|---|---|
| 20001 | `AUTHENTICATION_ERROR_API_KEY_NOT_FOUND` | API キーヘッダ欠落 | 起動時の設定漏れ。`RAKUTEN_API_KEY` が未設定。 |
| 20002 | `AUTHENTICATION_ERROR_INVALID_API_KEY` | API キーが無効 | キーの typo / 期限切れ / 権限不足。楽天管理画面で再発行。 |
| 20003 | `AUTHENTICATION_ERROR_NONCE_NOT_FOUND` | NONCE ヘッダ欠落 | `auth.go` のヘッダ生成バグなら調査。通常運用では発生しない。 |
| 20004 | `AUTHENTICATION_ERROR_INVALID_NONCE` | NONCE が無効 | 同一 NONCE の使い回しや時刻ズレで発生しうる。本システムの `nonce` は `time.Now().UnixNano()` ベースなので通常踏まないが、複数プロセスが同じ API キーを共有すると衝突する可能性がある。**現状リトライ対象外**。散発的に出る場合は nonce 衝突 / サーバー時刻ドリフトを疑う。 |
| 20005 | `AUTHENTICATION_ERROR_SIGNATURE_NOT_FOUND` | 署名ヘッダ欠落 | `auth.go` のバグ。通常運用では発生しない。 |
| 20006 | `AUTHENTICATION_ERROR_INVALID_SIGNATURE` | 署名が不正 | `API_SECRET` の typo / 改変箇所・method・path・query・body のいずれかが署名対象とズレている。`auth.go` のデバッグが必要。 |
| 20008 | `AUTHENTICATION_ERROR_INVALID_USER` | 無効なユーザー | アカウント凍結 / 停止の可能性。楽天サポートへ要問い合わせ。 |
| **20010** | **`AUTHENTICATION_ERROR_TOO_MANY_REQUESTS`** | **200ms 間隔制限違反 (レートリミット)** | **`retryOn20010` が 20010 のみ最大 3 回リトライ (300 → 600 → 1200 ms)。20010 は認証エラー分類だが実体はレートリミットである点に注意。** |

## 30000 番台: リクエストエラー

| コード | 名称 | 意味 | 本システムの扱い |
|---|---|---|---|
| 30010 | `REQUEST_ERROR_INVALID_USER` | 無効なユーザー | 20008 と同様、アカウント状態を確認。 |
| 30022 | `REQUEST_ERROR_INVALID_ORDER_TYPE` | 注文種別が無効 | `MARKET` 以外を投げていないか確認。本システムは成行のみ対応。 |
| 30056 | `REQUEST_ERROR_GET_ASSET_API_IS_NOT_AVAILABLE` | Asset API 使用不可 | 一時的にメンテ / 権限制限。`syncState` は warn のみで続行し、次回の定期同期で復旧を試みる。 |
| 30057 | `REQUEST_ERROR_GET_ORDER_API_IS_NOT_AVAILABLE` | Order 取得 API 使用不可 | 同上。 |
| 30058 | `REQUEST_ERROR_POST_ORDER_API_IS_NOT_AVAILABLE` | 注文投入 API 使用不可 | 発注系がこのエラーを返すと自動売買が詰まる。監視要。 |
| 30059 | `REQUEST_ERROR_DELETE_ORDER_API_IS_NOT_AVAILABLE` | 注文取消 API 使用不可 | キャンセル系。通常運用では未使用。 |
| 30060 | `REQUEST_ERROR_GET_TRADE_API_IS_NOT_AVAILABLE` | 約定履歴 API 使用不可 | `DailyPnLCalculator` がこれを踏むと pnl 計算が失敗する。`/api/v1/pnl` は直近の成功値を返すフォールバック有り。 |
| 30064 | `REQUEST_ERROR_INVALID_CURRENCY` | 通貨指定が無効 | JPY 以外を指定している場合。本システムは JPY 固定。 |
| 30106 | `REQUEST_ERROR_PUT_ORDER_API_IS_NOT_AVAILABLE` | 注文変更 API 使用不可 | 本システムは注文変更未使用。 |
| 30107 | `REQUEST_ERROR_GET_POSITION_API_IS_NOT_AVAILABLE` | ポジション取得 API 使用不可 | `syncState` の `GetPositions` がこれを踏むとポジション表示が更新されない。 |
| 30113 | `REQUEST_ERROR_GET_EQUITY_API_IS_NOT_AVAILABLE` | エクイティ取得 API 使用不可 | 本システムは未使用 API。 |
| 30116 | `REQUEST_ERROR_BUSINESS_DATE_NOT_FOUND` | 営業日が見つからない | 日次集計系 API で発生しうる。`DailyPnLCalculator` が要観察。 |
| 30122 | `REQUEST_ERROR_INVALID_API_USER` | API ユーザーが無効 | 20002 / 20008 と近い。キー自体は正しいが API 利用が停止されている可能性。 |

## 40000 番台

**公式ページには 40000 番台のエラーコードは掲載されていない。** HTTP の 4xx ステータスとは無関係。

## 50000 番台: 注文エラー

発注・決済系のエラーはすべてここに集約される。本システムは市場成行 (`MARKET`) 注文のみを扱うため、`ORDER_ERROR_INVALID_ORDER_TYPE` 系は理論上踏まない。

| コード | 名称 | 意味 | 本システムの扱い |
|---|---|---|---|
| 50003 | `ORDER_ERROR_ORDERBOOK_NOT_FOUND` | 板情報が取れない | 市場停止・銘柄停止の可能性。自動売買はスキップ。 |
| 50004 | `ORDER_ERROR_AMOUNT_OUT_OF_RANGE` | 数量が範囲外 | 最小単位未満 / 上限超過。`pipeline.go` の丸め (`math.Floor(amount*10000)/10000`) をすり抜けた場合に発生しうる。 |
| 50005 | `ORDER_ERROR_PRICE_NOT_FOUND` | 価格が取れない | 成行では通常発生しない。 |
| 50008 | `ORDER_ERROR_PRICE_OUT_OF_RANGE` | 価格が範囲外 | 同上。指値を使う場合に要注意。 |
| 50009 | `ORDER_ERROR_ORDER_NOT_FOUND` | 注文が見つからない | キャンセル対象が既に約定 / 取消済み。 |
| 50016 | `REQUEST_ERROR_USER_NOT_FOUND` | ユーザーが見つからない | 認証通過後にユーザーが見つからないのは楽天側の不整合。 |
| 50018 | `ORDER_ERROR_CURRENCY_CONFIG_NOT_FOUND` | 通貨設定が無い | 対象シンボルの設定が楽天側に無い。銘柄停止の可能性。 |
| 50020 | `ORDER_ERROR_AMOUNT_OUT_OF_MINMAX` | 数量が最小/最大レンジ外 | 50004 と類似。`TRADE_AMOUNT` / 価格次第で BTC 最小単位を下回るケース。 |
| 50021 | `ORDER_ERROR_AMOUNT_OVER_MAX_PER_DAY` | 1 日あたりの最大数量超過 | 楽天側の 1 日あたり上限に当たった。翌営業日まで待つしかない。 |
| 50023 | `ORDER_ERROR_INVALID_AMOUNT` | 数量が無効 | パース失敗・マイナス値など。 |
| 50026 | `ORDER_ERROR_INVALID_PRICE` | 価格が無効 | 成行では発生しない。 |
| 50034 | `REQUEST_ERROR_INVALID_USER_STATUS` | ユーザー状態が無効 | アカウント停止中など。 |
| 50035 | `ORDER_ERROR_CLOSE_POSITION_ID_NOT_SET` | 決済対象ポジション ID 未指定 | 決済注文で `positionId` が抜けている。本システムは `ClosePosition` で必ず ID を渡す。 |
| 50036 | `ORDER_ERROR_LEVERAGE_OUT_OF_MINMAX` | レバレッジが範囲外 | 楽天の許可レバレッジ外。 |
| 50037 | `ORDER_ERROR_INVALID_OPEN_CLOSE_ORDER_BEHAVIOR` | 新規/決済指定が不正 | open/close フラグの組合せ異常。 |
| 50038 | `ORDER_ERROR_INVALID_ORDER_COMBINATION` | 注文組合せが不正 | 複数条件注文の組合せ異常。本システム未使用。 |
| 50040 | `ORDER_ERROR_INVALID_CLOSE_AMOUNT` | 決済数量が不正 | 決済数量 > 保有数量など。 |
| 50041 | `ORDER_ERROR_INVALID_OPEN_CLOSE_ORDER_SIDE` | 新規/決済の売買方向が不正 | 例: BUY ポジションを BUY で決済しようとした等。 |
| 50042 | `ORDER_ERROR_OPEN_POSITION_NOT_FOUND` | 決済対象のオープンポジションが無い | 既に決済済みのポジションを再度決済しようとした。`syncState` の遅延で発生しうる。 |
| 50043 | `ORDER_ERROR_CLOSE_AMOUNT_EXCEED_POSITION` | 決済数量がポジションを超過 | 同上。 |
| 50044 | `ORDER_ERROR_CLOSE_ONLY` | 決済専用モード | 楽天側が新規建てを停止中 (相場急変時など)。 |
| 50046 | `ORDER_ERROR_POSITION_AMOUNT_OVER_MAX` | ポジション数量上限超過 | アカウント単位の総建玉上限。 |
| 50047 | `ORDER_ERROR_LOSS_CUT_MARGIN_MAINTENANCE_RATE` | ロスカット (維持証拠金率) | 維持率不足。強制決済対象。 |
| 50048 | `ORDER_ERROR_INSUFFICIENT_USABLE_AMOUNT` | 利用可能残高不足 | 必要証拠金 > 有効証拠金。本システム側でも `RiskManager.CheckOrder` が先に弾くが、楽天側の計算が優先される。 |
| 50049 | `ORDER_ERROR_LESS_THAN_MIN_CHANGE_SPAN` | 注文変更の最小間隔未満 | 変更注文は本システム未使用。 |
| 50050 | `ORDER_ERROR_INVALID_CHANGE_AMOUNT` | 変更数量が不正 | 同上。 |
| 50051 | `ORDER_ERROR_COMMON` | 共通注文エラー | 詳細不明の一般エラー。`RawResponse` の本文を見る。 |
| 50052 | `ORDER_ERROR_COMMON_CANCEL` | 共通取消エラー | キャンセル時の一般エラー。 |
| 50053 | `ORDER_ERROR_PARTIAL_FILL_AMOUNT_CHANGE` | 部分約定済み注文の数量変更不可 | 注文変更系。未使用。 |
| 50054 | `ORDER_ERROR_NO_ORDER_CHANGE` | 注文変更点が無い | 同上。 |
| 50055 | `ORDER_ERROR_INVALID_CLOSE_ORDER_FIFO_EXISTS` | FIFO 決済順序違反 | FIFO ルールでは古いポジションから順に決済する必要がある。本システムは pipeline からの決済でポジション選択を明示しているため通常踏まない。 |
| 50056 | `ORDER_ERROR_INVALID_FIFO_ORDER_CROSS_EXISTS` | FIFO クロス注文の存在 | 同上。 |

## 本システムでの扱いの方針

### リトライする/しないの判断基準

- **20010 (レートリミット) のみ自動リトライ** — 実質的にジッタ起因の一時エラーで、再送すれば成功する確率が高いため。
- **それ以外のエラーはリトライしない** — 10001/30056 のような楽天側 API 一時停止系も、定期同期ループ (`runStateSyncLoop`, 15 秒間隔) に任せれば次サイクルで自動的に再試行される。`retryOn20010` の backoff では復旧しない。
- **発注系 (50000 番台)** は業務的な失敗なので、リトライしてはいけない (二重発注を招く)。`CreateOrderRaw` で `submitted` か `failed` かを構造化判定し、`ClientOrderRepo` で idempotency を確保する。

### エラーコードを error 文字列からパースする

楽天の `DoRaw` はエラー本文をそのまま `fmt.Errorf("API error (status %d): %s", statusCode, string(body))` の形で文字列化する。そのため error からコードを取り出したい場合は以下のいずれか:

1. `strings.Contains(err.Error(), \`"code":20010\`)` のような部分一致 — `retryOn20010` はこの方式。
2. 構造化が必要なら `DoPrivateRaw` を使って `body []byte` を直接 `json.Unmarshal` する — `CreateOrderRaw` がこの方式。

後者のほうが厳密だが、単一コードだけ気にすればよい場合は前者で十分。

### 未知のエラーコードに遭遇したら

1. 本ドキュメントを更新する前に、**公式ページで最新の定義を必ず確認** する (楽天は予告なくコードを追加する可能性がある)。
2. 本システム内でどう振る舞うべきかを判断してから、該当箇所 (pipeline / order executor / dailyPnL など) に warn ログを追加する。
3. リトライ対象にする場合は `retryOn20010` を **単一目的のまま** にして、別ヘルパー (`retryOnTransient` 等) を新設するのが望ましい。安易に判定条件を広げないこと。
