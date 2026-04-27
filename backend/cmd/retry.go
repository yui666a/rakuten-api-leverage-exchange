package main

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// retryBackoffs は retryOn20010 が各リトライの前に挟む待ち時間。
// 長さ = 最大リトライ回数。初回の失敗からこの長さ分までリトライする。
//
// 旧実装 (発射時刻起点の 220ms) では RTT ジッタで 20010 を散発的に踏んでいた
// ため active な防御として機能していた。
// 現行 rest_client は「応答完了 → 220ms → 次発射」モデルに変更され、楽天視点
// の受信間隔が構造的に 200ms を超える設計になったので、20010 はほぼ発生しない。
// 本リトライは念のためのフェイルセーフとして残す:
//   - 楽天サーバ側のジッタや一時的不整合
//   - 別プロセスが同じ API キーを共有してしまった場合
//   - クライアントの time.Sleep 精度劣化
// などで万一 20010 を踏んだときに同期処理を継続させるための保険。
// 300→600→1200ms と指数で伸ばす。
var retryBackoffs = []time.Duration{
	300 * time.Millisecond,
	600 * time.Millisecond,
	1200 * time.Millisecond,
}

// isRateLimitError は楽天 API の 20010 (AUTHENTICATION_ERROR_TOO_MANY_REQUESTS) を
// 判定する。楽天の DoRaw はエラー本文をそのまま文字列化して error に載せるので、
// `"code":20010` が含まれていれば 20010 とみなせる。
//
// 将来 楽天側が整形を変えてきたら複数パターンへのマッチに広げる余地あり。
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), `"code":20010`)
}

// retryOn20010 は fn を実行し、20010 エラーのときだけ最大 len(retryBackoffs) 回
// リトライする。20010 以外のエラー・成功時は即座に結果を返す。
//
// sleep は time.Sleep をそのまま使うことを想定しているが、テストで時間を
// 消費させないため引数で注入できるようにしている。
//
// ctx が死んでいたらリトライ前に中断し、最後のエラー (もしくは ctx.Err) を返す。
func retryOn20010(ctx context.Context, sleep func(time.Duration), fn func() error) error {
	var err error
	for attempt := 0; attempt <= len(retryBackoffs); attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isRateLimitError(err) {
			return err
		}
		if attempt == len(retryBackoffs) {
			// 最後の試行が 20010 だった。これ以上リトライしない。
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		delay := retryBackoffs[attempt]
		slog.Warn("pipeline: rakuten rate limit (20010), retrying",
			"attempt", attempt+1,
			"next_delay_ms", delay.Milliseconds())
		sleep(delay)
	}
	return err
}
