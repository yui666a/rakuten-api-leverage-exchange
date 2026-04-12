package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// noopSleep はテスト用に time.Sleep を置き換える no-op 関数。
// retryOn20010 は本来バックオフ待ちに time.After を使うが、テスト時は
// 呼び出し経路を記録するだけに差し替えたいのでスリープ関数を DI する。
func noopSleep(d time.Duration) {}

// err20010 は楽天 API の 20010 エラーレスポンスと同じ文字列を含むエラーを返す。
// 本物は rest_client.go の do() が fmt.Errorf("API error (status 500): %s", body) を
// さらに GetAssets 側が "GetAssets: %w" でラップした形になる。
func err20010(label string) error {
	return fmt.Errorf("%s: API error (status 500): {\"code\":20010}", label)
}

func TestRetryOn20010_NonRateLimitErrorDoesNotRetry(t *testing.T) {
	calls := 0
	err := retryOn20010(context.Background(), noopSleep, func() error {
		calls++
		return errors.New("some other error")
	})
	if err == nil || err.Error() != "some other error" {
		t.Fatalf("expected original error to propagate, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected fn to be called exactly once for non-20010 error, got %d", calls)
	}
}

func TestRetryOn20010_SuccessAfterTwoRateLimitErrors(t *testing.T) {
	calls := 0
	err := retryOn20010(context.Background(), noopSleep, func() error {
		calls++
		if calls < 3 {
			return err20010("GetAssets")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected fn to be called 3 times, got %d", calls)
	}
}

func TestRetryOn20010_ExhaustsRetriesAndReturnsLastError(t *testing.T) {
	calls := 0
	err := retryOn20010(context.Background(), noopSleep, func() error {
		calls++
		return err20010("GetAssets")
	})
	if err == nil {
		t.Fatalf("expected error after exhausting retries, got nil")
	}
	if calls != 4 {
		t.Fatalf("expected fn to be called 4 times (1 initial + 3 retries), got %d", calls)
	}
}

func TestRetryOn20010_ContextCancelStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 事前にキャンセル済み

	calls := 0
	err := retryOn20010(ctx, noopSleep, func() error {
		calls++
		return err20010("GetAssets")
	})
	if err == nil {
		t.Fatalf("expected non-nil error when ctx is already cancelled")
	}
	if calls < 1 {
		t.Fatalf("expected fn to be called at least once before ctx check, got %d", calls)
	}
	// ctx が既に死んでいれば 1 回で抜けるはず（backoff 待ちで ctx.Done() を見る設計）
	if calls > 1 {
		t.Fatalf("expected no retries after ctx cancel, got %d calls", calls)
	}
}
