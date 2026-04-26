package rakuten

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRESTClient_RateLimiting(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "key", "secret")

	ctx := context.Background()

	start := time.Now()
	_, _ = client.DoPublic(ctx, "GET", "/test1", "", nil)
	_, _ = client.DoPublic(ctx, "GET", "/test2", "", nil)
	elapsed := time.Since(start)

	if elapsed < 200*time.Millisecond {
		t.Fatalf("rate limiting not working: 2 requests completed in %v, expected >= 200ms", elapsed)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
}

func TestRESTClient_DoPublic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/ticker" {
			t.Fatalf("expected /api/v1/ticker, got %s", r.URL.Path)
		}
		if r.URL.RawQuery != "symbolId=7" {
			t.Fatalf("expected symbolId=7, got %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"last":5000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ctx := context.Background()

	body, err := client.DoPublic(ctx, "GET", "/api/v1/ticker", "symbolId=7", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("body should not be empty")
	}
}

func TestRESTClient_DoPrivate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("API-KEY")
		nonce := r.Header.Get("NONCE")
		signature := r.Header.Get("SIGNATURE")

		if apiKey != "test-key" {
			t.Fatalf("expected API-KEY 'test-key', got '%s'", apiKey)
		}
		if nonce == "" {
			t.Fatal("NONCE header should not be empty")
		}
		if signature == "" {
			t.Fatal("SIGNATURE header should not be empty")
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"currency":"JPY","onhandAmount":10000}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "test-key", "test-secret")
	ctx := context.Background()

	body, err := client.DoPrivate(ctx, "GET", "/api/v1/asset", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("body should not be empty")
	}
}

// TestRESTClient_HighPriorityOvertakesNormal は、normal が複数並んでいる
// ところに high が割り込んだとき、high が「次の 220ms スロット」で先に通る
// ことを検証する。
//
// シナリオ:
//   1. normal A を投げる (即時、t=0 で発射)
//   2. すぐに normal B, normal C を順番に投げる (220, 440 で発射されるはず)
//   3. その直後に high D を投げる (B が出る前に割り込み: 220 で D, 440 で B)
//
// 期待される到達順: A, D, B, C  (D が B/C を追い越す)
func TestRESTClient_HighPriorityOvertakesNormal(t *testing.T) {
	var (
		mu       sync.Mutex
		arrivals []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrivals = append(arrivals, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ctx := context.Background()

	var wg sync.WaitGroup

	// A: 最初の normal、即時通過 (lastCall がまだないので t=0 で発射)。
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = client.DoPublicWithPriority(ctx, "GET", "/A", "", nil, PriorityNormal)
	}()

	// 5 ms 待って、最初のリクエストが先に発射されたことを保証する。
	time.Sleep(5 * time.Millisecond)

	// B, C を normal で並べる (B = 220ms スロット、C = 440ms スロットのはず)。
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = client.DoPublicWithPriority(ctx, "GET", "/B", "", nil, PriorityNormal)
	}()
	go func() {
		defer wg.Done()
		_, _ = client.DoPublicWithPriority(ctx, "GET", "/C", "", nil, PriorityNormal)
	}()

	// 5 ms 後 (B/C はまだ待ち中) に high D を割り込ませる。
	time.Sleep(5 * time.Millisecond)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = client.DoPublicWithPriority(ctx, "GET", "/D", "", nil, PriorityHigh)
	}()

	wg.Wait()

	mu.Lock()
	got := append([]string(nil), arrivals...)
	mu.Unlock()

	if len(got) != 4 {
		t.Fatalf("expected 4 arrivals, got %v", got)
	}
	// A は最初。
	if got[0] != "/A" {
		t.Fatalf("expected /A first, got %v", got)
	}
	// D は 2 番目 (high が normal を追い越す)。
	if got[1] != "/D" {
		t.Fatalf("expected /D second (high priority overtakes normal), got %v", got)
	}
	// B, C は順不同で残り。
	rest := []string{got[2], got[3]}
	if !((rest[0] == "/B" && rest[1] == "/C") || (rest[0] == "/C" && rest[1] == "/B")) {
		t.Fatalf("expected /B and /C in any order at positions 2-3, got %v", got)
	}
}

// TestRESTClient_OrderBurstShareYieldsToNormal は、high が連続で
// orderBurstShare 個まで通った後に必ず 1 個 normal が混ざる
// (= スタベーションしない) ことを検証する。
//
// 設計値: orderBurstShare = 5 (5 連続 high を捌いたら 1 個 normal を通す)。
func TestRESTClient_OrderBurstShareYieldsToNormal(t *testing.T) {
	var (
		mu       sync.Mutex
		arrivals []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrivals = append(arrivals, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ctx := context.Background()

	// 最初のリクエストは即時通過するため、まず空打ち (= "/seed") で
	// 内部の lastCall を進めてから本体を投げる。
	_, _ = client.DoPublicWithPriority(ctx, "GET", "/seed", "", nil, PriorityNormal)

	mu.Lock()
	arrivals = nil
	mu.Unlock()

	var wg sync.WaitGroup
	// normal 1 個を先に並べる (これが「割り込まれる側」)。
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = client.DoPublicWithPriority(ctx, "GET", "/N", "", nil, PriorityNormal)
	}()
	time.Sleep(5 * time.Millisecond) // N が確実にキューに入るまで待つ

	// high を 6 連続 (= orderBurstShare(5) + 1) 並べる。
	for i := 0; i < 6; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := "/H" + string(rune('0'+i))
			_, _ = client.DoPublicWithPriority(ctx, "GET", path, "", nil, PriorityHigh)
		}()
		time.Sleep(2 * time.Millisecond) // 順序を安定させる
	}

	wg.Wait()

	mu.Lock()
	got := append([]string(nil), arrivals...)
	mu.Unlock()

	if len(got) != 7 {
		t.Fatalf("expected 7 arrivals, got %v", got)
	}

	// 最初の 5 個は high (H0..H4) のはず。次に N が割り込み、最後に H5。
	for i := 0; i < 5; i++ {
		if len(got[i]) < 2 || got[i][1] != 'H' {
			t.Fatalf("expected first %d arrivals to be high-priority, got %v", 5, got)
		}
	}
	if got[5] != "/N" {
		t.Fatalf("expected /N at position 6 (after %d high bursts), got %v", 5, got)
	}
	if got[6] != "/H5" {
		t.Fatalf("expected /H5 last, got %v", got)
	}
}

// TestRESTClient_PriorityRespectsRateLimit は、優先度に関係なく
// 200ms 間隔は守られることを検証する。
func TestRESTClient_PriorityRespectsRateLimit(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ctx := context.Background()

	start := time.Now()
	_, _ = client.DoPublicWithPriority(ctx, "GET", "/h1", "", nil, PriorityHigh)
	_, _ = client.DoPublicWithPriority(ctx, "GET", "/h2", "", nil, PriorityHigh)
	elapsed := time.Since(start)

	if elapsed < 200*time.Millisecond {
		t.Fatalf("rate limit violated for high-priority sequence: 2 reqs in %v", elapsed)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestRESTClient_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":"INVALID_PARAMETER","message":"invalid symbolId"}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ctx := context.Background()

	_, err := client.DoPublic(ctx, "GET", "/api/v1/ticker", "symbolId=999", nil)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}
