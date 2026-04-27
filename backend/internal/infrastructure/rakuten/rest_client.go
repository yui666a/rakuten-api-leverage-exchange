package rakuten

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Priority はレートリミットキューにおける優先度。
// 発注 (CreateOrder / CancelOrder) は PriorityHigh、それ以外は PriorityNormal。
type Priority int

const (
	PriorityNormal Priority = 0
	PriorityHigh   Priority = 1
)

// rateInterval は「直前のリクエストの応答が返ってきてから次を発射するまで」に
// 開ける最小間隔。
//
// 楽天 Private API は 1 ユーザーあたり 200ms 制限。応答完了 (= 楽天サーバが
// レスポンスを送出した時刻 ≒ 楽天受信時刻 + サーバ処理時間) を起点にすれば、
// `応答完了 → こちらに到達 → 220ms 待機 → こちら発射 → 楽天受信`
// となり、楽天視点の受信間隔は必ず `220ms + 往路 RTT` ≧ 200ms を満たす。
//
// 旧実装は「発射時刻」を起点にしていたため、ネットワーク RTT のジッタで
// 楽天視点の受信間隔が 200ms を割り、20010 を散発的に踏んでいた。
const rateInterval = 220 * time.Millisecond

// orderBurstShare は high が連続で何個まで normal を追い越せるかの上限。
// この回数 high を捌いたら、normal が 1 個でも待っていれば優先で通す
// (= スタベーション防止)。発注バーストは通常 1〜2 個で収まるので 5 は十分余裕。
const orderBurstShare = 5

// rateJob は dispatch goroutine に発射してもらう 1 個のリクエストを表す。
// 呼び出し元 goroutine は job を投げて reply を待つだけで、HTTP 実行自体は
// dispatch goroutine が行う。これにより全リクエストが「応答完了から 220ms」で
// 直列化される。
type rateJob struct {
	// リクエスト構築に必要な情報
	method        string
	path          string
	query         string
	body          []byte
	authenticated bool
	ctx           context.Context

	// 完了通知。dispatch goroutine が HTTP 実行後にこの channel に結果を送る。
	// バッファ 1 にしておくことで、呼び出し元 goroutine が ctx キャンセル等で
	// 既に立ち去っていても dispatch 側の送信がブロックされない。
	reply chan httpExchange
}

func newRateJob(ctx context.Context, method, path, query string, body []byte, authenticated bool) *rateJob {
	return &rateJob{
		method:        method,
		path:          path,
		query:         query,
		body:          body,
		authenticated: authenticated,
		ctx:           ctx,
		reply:         make(chan httpExchange, 1),
	}
}

type RESTClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string

	highQ   chan *rateJob
	normalQ chan *rateJob
}

func NewRESTClient(baseURL, apiKey, apiSecret string) *RESTClient {
	c := &RESTClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		highQ:      make(chan *rateJob, 256),
		normalQ:    make(chan *rateJob, 256),
	}
	go c.dispatchLoop()
	return c
}

// dispatchLoop は 1 個ずつジョブを取り出して HTTP を実行する単一 goroutine。
//
// 動作:
//   - 1 ジョブ目: キューに job が入った瞬間にすぐ実行 (初回 wait なし)
//   - それ以降: 直前のジョブの応答完了から rateInterval 経過するまで待ってから
//     次のジョブを取り出して実行
//   - high が並んでいれば優先で通す。ただし orderBurstShare 連続したら
//     normal が居る限り 1 個 normal を通す (フェアネス)
//
// HTTP 実行自体をこの goroutine で行うため、楽天サーバー視点での受信間隔は
// 「応答完了 → 220ms → 次の発射」となり、200ms 制限を構造的に満たす。
//
// goroutine は RESTClient が GC されるまで生きるが、本サービスはプロセスライフタイム
// 全体で 1 個の RESTClient を共有するため、明示的な停止は実装しない。
func (c *RESTClient) dispatchLoop() {
	var (
		lastCompleted time.Time
		highInRow     int
	)
	for {
		// レートインターバルを満たすまで待つ。初回 (lastCompleted ゼロ値) は即時。
		if !lastCompleted.IsZero() {
			if wait := rateInterval - time.Since(lastCompleted); wait > 0 {
				time.Sleep(wait)
			}
		}
		// 待機を終えた直後に「いま並んでいる中で誰を通すか」を決める。
		job := c.pickNext(&highInRow)

		// 呼び出し元の ctx が既に死んでいたら HTTP は打たない。
		// reply に ctx エラーを返してスキップ。
		if err := job.ctx.Err(); err != nil {
			job.reply <- httpExchange{transportError: err}
			// このジョブは楽天 API を叩いていないので lastCompleted は更新しない。
			// (= 次のジョブはすぐに pick されてよい)
			continue
		}

		// HTTP を同期実行。完了時刻を次のレートインターバル基準にする。
		ex := c.execute(job)
		lastCompleted = time.Now()
		job.reply <- ex
	}
}

// pickNext は次に発射するジョブを 1 個選ぶ。
// orderBurstShare の境界では強制的に normal を選ぶ。
func (c *RESTClient) pickNext(highInRow *int) *rateJob {
	// high のスタベーション保護: 5 連続したら normal を最優先で拾う。
	if *highInRow >= orderBurstShare {
		select {
		case j := <-c.normalQ:
			*highInRow = 0
			return j
		default:
			// normal は居ない。high のままでよい。
		}
	}
	// 通常時は high 優先。
	select {
	case j := <-c.highQ:
		*highInRow++
		return j
	default:
	}
	// high なし。normal をブロッキング取得し、もし最後の瞬間に high が
	// 入ってきても normal が公平に通る (チャネル受信は select 順で安定)。
	select {
	case j := <-c.normalQ:
		*highInRow = 0
		return j
	case j := <-c.highQ:
		*highInRow++
		return j
	}
}

// enqueue は job を該当キューに積み、応答 (もしくは ctx キャンセル) を待つ。
func (c *RESTClient) enqueue(job *rateJob, prio Priority) httpExchange {
	q := c.normalQ
	if prio == PriorityHigh {
		q = c.highQ
	}
	// キューに積む。容量 256 なので通常はブロックしないが、極端な詰まりが
	// 発生した場合は ctx キャンセルで抜ける。
	select {
	case q <- job:
	case <-job.ctx.Done():
		return httpExchange{transportError: job.ctx.Err()}
	}
	// dispatch goroutine が HTTP 実行を終えたら reply が来る。
	select {
	case ex := <-job.reply:
		return ex
	case <-job.ctx.Done():
		// ctx が先に死んだ。dispatch 側の reply はバッファ 1 なので
		// あとから書き込まれてもブロックしない。本 goroutine はもう
		// 結果を読まない。
		return httpExchange{transportError: job.ctx.Err()}
	}
}

// DoPublic / DoPrivate は normal 優先度で動作する後方互換ラッパー。
func (c *RESTClient) DoPublic(ctx context.Context, method, path, query string, body []byte) ([]byte, error) {
	return c.DoPublicWithPriority(ctx, method, path, query, body, PriorityNormal)
}

func (c *RESTClient) DoPrivate(ctx context.Context, method, path, query string, body []byte) ([]byte, error) {
	return c.DoPrivateWithPriority(ctx, method, path, query, body, PriorityNormal)
}

func (c *RESTClient) DoPrivateRaw(ctx context.Context, method, path, query string, body []byte) (statusCode int, respBody []byte, transportErr error) {
	return c.DoPrivateRawWithPriority(ctx, method, path, query, body, PriorityNormal)
}

// DoPublicWithPriority は明示的に優先度を指定する Public API 呼び出し。
// テスト用にも使うが、実コードで Public を high にする場面はない。
func (c *RESTClient) DoPublicWithPriority(ctx context.Context, method, path, query string, body []byte, prio Priority) ([]byte, error) {
	return c.do(ctx, method, path, query, body, false, prio)
}

// DoPrivateWithPriority は明示的に優先度を指定する Private API 呼び出し。
// 発注経路から PriorityHigh で呼ぶことで、参照系で詰まったキューを追い越す。
func (c *RESTClient) DoPrivateWithPriority(ctx context.Context, method, path, query string, body []byte, prio Priority) ([]byte, error) {
	return c.do(ctx, method, path, query, body, true, prio)
}

// DoPrivateRawWithPriority は DoPrivateRaw の優先度指定版。
// 発注 (CreateOrderRaw) はこれを PriorityHigh で叩く。
func (c *RESTClient) DoPrivateRawWithPriority(ctx context.Context, method, path, query string, body []byte, prio Priority) (statusCode int, respBody []byte, transportErr error) {
	ex := c.doRaw(ctx, method, path, query, body, true, prio)
	return ex.statusCode, ex.body, ex.transportError
}

// httpExchange は do() の構造化版。トランスポート失敗・非 2xx・本文を区別して返す。
type httpExchange struct {
	statusCode     int
	body           []byte
	transportError error
}

func (c *RESTClient) do(ctx context.Context, method, path, query string, body []byte, authenticated bool, prio Priority) ([]byte, error) {
	ex := c.doRaw(ctx, method, path, query, body, authenticated, prio)
	if ex.transportError != nil {
		return nil, ex.transportError
	}
	if ex.statusCode < 200 || ex.statusCode >= 300 {
		return nil, fmt.Errorf("API error (status %d): %s", ex.statusCode, string(ex.body))
	}
	return ex.body, nil
}

func (c *RESTClient) doRaw(ctx context.Context, method, path, query string, body []byte, authenticated bool, prio Priority) httpExchange {
	job := newRateJob(ctx, method, path, query, body, authenticated)
	return c.enqueue(job, prio)
}

// execute は dispatch goroutine から呼ばれ、楽天 API への HTTP 呼び出しを
// 同期実行する。rateInterval の起点になる「応答完了時刻」を返すため、
// goroutine をまたがず単一スレッドで完結させる必要がある。
func (c *RESTClient) execute(job *rateJob) httpExchange {
	url := c.baseURL + job.path
	if job.query != "" {
		url += "?" + job.query
	}

	var bodyReader io.Reader
	if job.body != nil {
		bodyReader = strings.NewReader(string(job.body))
	}

	req, err := http.NewRequestWithContext(job.ctx, job.method, url, bodyReader)
	if err != nil {
		return httpExchange{transportError: fmt.Errorf("failed to create request: %w", err)}
	}

	if job.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if job.authenticated {
		headers := GenerateAuthHeaders(c.apiKey, c.apiSecret, job.method, job.path, job.query, string(job.body))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return httpExchange{transportError: fmt.Errorf("request failed: %w", err)}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpExchange{
			statusCode:     resp.StatusCode,
			transportError: fmt.Errorf("failed to read response body: %w", err),
		}
	}

	return httpExchange{
		statusCode: resp.StatusCode,
		body:       respBody,
	}
}
