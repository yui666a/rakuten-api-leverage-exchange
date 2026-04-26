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

// rateInterval は連続するリクエスト間に開けるべき最小間隔。
// 楽天 Private API は 200ms 制限のため、サーバー側ジッタを吸収するマージンを 20ms 載せる。
const rateInterval = 220 * time.Millisecond

// orderBurstShare は high が連続で何個まで normal を追い越せるかの上限。
// この回数 high を捌いたら、normal が 1 個でも待っていれば優先で通す
// (= スタベーション防止)。発注バーストは通常 1〜2 個で収まるので 5 は十分余裕。
const orderBurstShare = 5

// rateRequest は 1 個のリクエストが「次にスロットを使ってよい」通知を
// 受け取るための受信箱。1 バッファのチャネルにすることで、ticker goroutine の
// 送信が必ず非ブロックで完了する。
type rateRequest struct {
	notify chan struct{}
}

func newRateRequest() *rateRequest {
	return &rateRequest{notify: make(chan struct{}, 1)}
}

type RESTClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string

	highQ   chan *rateRequest
	normalQ chan *rateRequest
}

func NewRESTClient(baseURL, apiKey, apiSecret string) *RESTClient {
	c := &RESTClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		highQ:      make(chan *rateRequest, 256),
		normalQ:    make(chan *rateRequest, 256),
	}
	go c.dispatchLoop()
	return c
}

// dispatchLoop は 220ms 間隔で「次に発射してよい」権を 1 個ずつ発行する。
//
// 動作:
//   - 1 リクエスト目: キューに requester が入った瞬間にすぐ発射 (初回 wait なし)
//   - それ以降: 直前の発射時刻から rateInterval 経過するまで待ってから「次の req を取り出して」発行
//   - high が並んでいれば優先で通す。ただし orderBurstShare 連続したら
//     normal が居る限り 1 個 normal を通す (フェアネス)
//
// 重要: 次の req を取り出すタイミングは「rateInterval 経過後」。これにより
// 直前の発射 → 待機中の間に投入された high が、待機明けでちゃんと拾える。
//
// goroutine は RESTClient が GC されるまで生きるが、本サービスはプロセスライフタイム
// 全体で 1 個の RESTClient を共有するため、明示的な停止は実装しない。
func (c *RESTClient) dispatchLoop() {
	var (
		lastFired time.Time
		highInRow int
	)
	for {
		// レートインターバルを満たすまで待つ。初回 (lastFired ゼロ値) は即時。
		if !lastFired.IsZero() {
			if wait := rateInterval - time.Since(lastFired); wait > 0 {
				time.Sleep(wait)
			}
		}
		// 待機を終えた直後に「いま並んでいる中で誰を通すか」を決める。
		req := c.pickNext(&highInRow)
		// requester に「いまから発射してよい」通知。
		req.notify <- struct{}{}
		lastFired = time.Now()
	}
}

// pickNext は次に発射する requester を 1 個選ぶ。
// orderBurstShare の境界では強制的に normal を選ぶ。
func (c *RESTClient) pickNext(highInRow *int) *rateRequest {
	// high のスタベーション保護: 5 連続したら normal を最優先で拾う。
	if *highInRow >= orderBurstShare {
		select {
		case r := <-c.normalQ:
			*highInRow = 0
			return r
		default:
			// normal は居ない。high のままでよい。
		}
	}
	// 通常時は high 優先。
	select {
	case r := <-c.highQ:
		*highInRow++
		return r
	default:
	}
	// high なし。normal をブロッキング取得し、もし最後の瞬間に high が
	// 入ってきても normal が公平に通る (チャネル受信は select 順で安定)。
	select {
	case r := <-c.normalQ:
		*highInRow = 0
		return r
	case r := <-c.highQ:
		*highInRow++
		return r
	}
}

// waitForSlot は当該リクエストの「次の 220ms スロット」を待つ。
// ctx が死んだ場合は ctx.Err を返す。
func (c *RESTClient) waitForSlot(ctx context.Context, prio Priority) error {
	req := newRateRequest()
	q := c.normalQ
	if prio == PriorityHigh {
		q = c.highQ
	}
	select {
	case q <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-req.notify:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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
	if err := c.waitForSlot(ctx, prio); err != nil {
		return httpExchange{transportError: err}
	}

	url := c.baseURL + path
	if query != "" {
		url += "?" + query
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return httpExchange{transportError: fmt.Errorf("failed to create request: %w", err)}
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if authenticated {
		headers := GenerateAuthHeaders(c.apiKey, c.apiSecret, method, path, query, string(body))
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

