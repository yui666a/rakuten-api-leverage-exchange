// Package orderretry は発注呼び出し (CreateOrderRaw) を 20010 (rate limit)
// のときだけ安全にリトライするヘルパーを提供する。
//
// 通常の参照系は cmd/retry.go の retryOn20010 を使うが、発注は二重発注リスクが
// あるため文字列マッチでは判定しない: CreateOrderOutcome の構造化情報
// (HTTPError + RawResponse の JSON code フィールド) だけを見て、
// 「楽天側で受理されなかった」と確証できる場合のみリトライする。
package orderretry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// Backoffs は各リトライ前に挟む待ち時間。長さ = 最大リトライ回数。
// 参照系の retryOn20010 と同じ値 (300/600/1200ms) を採用。
// 楽天 API の 200ms 制限 + 220ms クライアントマージンに対し、再送までに
// 余裕を持たせる目的。
var Backoffs = []time.Duration{
	300 * time.Millisecond,
	600 * time.Millisecond,
	1200 * time.Millisecond,
}

// OnRateLimit は fn を最大 len(Backoffs) 回リトライする。リトライするのは
// CreateOrderOutcome が「楽天側で 20010 として受理拒否されたことが構造的に
// 確証できる」ケースのみ。
//
// リトライしない条件 (= 二重発注リスク):
//   - 成功 (HTTPStatus 2xx かつ Orders 取得済み)
//   - TransportError (楽天到達状態が不明)
//   - HTTPError だが 20010 以外
//   - HTTPStatus 2xx だが ParseError あり (楽天は受理した可能性)
//   - fn 自体が error を返した (marshal 失敗等)
//
// sleep は time.Sleep と互換だが、テストで時間を消費しないよう DI 可能にしている。
func OnRateLimit(
	ctx context.Context,
	sleep func(time.Duration),
	fn func() (repository.CreateOrderOutcome, error),
) (repository.CreateOrderOutcome, error) {
	var (
		out repository.CreateOrderOutcome
		err error
	)
	for attempt := 0; attempt <= len(Backoffs); attempt++ {
		out, err = fn()
		if err != nil {
			return out, err
		}
		if !isRateLimitOutcome(out) {
			return out, nil
		}
		if attempt == len(Backoffs) {
			return out, nil
		}
		if ctx.Err() != nil {
			return out, nil
		}
		delay := Backoffs[attempt]
		slog.Warn("order: rakuten rate limit (20010), retrying",
			"attempt", attempt+1,
			"next_delay_ms", delay.Milliseconds())
		sleep(delay)
	}
	return out, nil
}

// isRateLimitOutcome は CreateOrderOutcome が 20010 (rate limit) で楽天側に
// 受理されなかったことを構造化情報のみで確証できる場合に true を返す。
//
// 「文字列に "20010" が含まれる」だけでは不十分: 発注用は二重発注リスクを
// 避けるため HTTPError + パース可能な JSON ボディ + code == 20010 の3つを
// 揃えてはじめてリトライ対象とする。
func isRateLimitOutcome(out repository.CreateOrderOutcome) bool {
	if out.HTTPError == nil {
		return false
	}
	if len(out.RawResponse) == 0 {
		return false
	}
	var probe struct {
		Code json.Number `json:"code"`
	}
	dec := json.NewDecoder(bytes.NewReader(out.RawResponse))
	dec.UseNumber()
	if err := dec.Decode(&probe); err != nil {
		return false
	}
	return probe.Code.String() == "20010"
}
