package orderretry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

func noopSleep(time.Duration) {}

func outcome20010() repository.CreateOrderOutcome {
	body := []byte(`{"code":20010,"message":"too many requests"}`)
	return repository.CreateOrderOutcome{
		HTTPStatus:  500,
		RawResponse: body,
		HTTPError:   fmt.Errorf("API error (status 500): %s", string(body)),
	}
}

func outcomeBusinessError() repository.CreateOrderOutcome {
	body := []byte(`{"code":50048,"message":"insufficient amount"}`)
	return repository.CreateOrderOutcome{
		HTTPStatus:  400,
		RawResponse: body,
		HTTPError:   fmt.Errorf("API error (status 400): %s", string(body)),
	}
}

func outcomeSuccess() repository.CreateOrderOutcome {
	return repository.CreateOrderOutcome{
		HTTPStatus: 200,
		Orders:     []entity.Order{{ID: 12345}},
	}
}

func outcomeTransport() repository.CreateOrderOutcome {
	return repository.CreateOrderOutcome{
		HTTPStatus:     0,
		TransportError: errors.New("dial tcp: i/o timeout"),
	}
}

func TestOnRateLimit_RetriesOn20010ThenSucceeds(t *testing.T) {
	calls := 0
	got, err := OnRateLimit(context.Background(), noopSleep, func() (repository.CreateOrderOutcome, error) {
		calls++
		if calls < 3 {
			return outcome20010(), nil
		}
		return outcomeSuccess(), nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.HTTPStatus != 200 {
		t.Fatalf("expected final outcome to be the successful one, got status %d", got.HTTPStatus)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (2 retries + success), got %d", calls)
	}
}

func TestOnRateLimit_DoesNotRetryOnSuccess(t *testing.T) {
	calls := 0
	got, err := OnRateLimit(context.Background(), noopSleep, func() (repository.CreateOrderOutcome, error) {
		calls++
		return outcomeSuccess(), nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.HTTPStatus != 200 {
		t.Fatalf("expected status 200, got %d", got.HTTPStatus)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 call on success, got %d", calls)
	}
}

func TestOnRateLimit_DoesNotRetryOnTransportError(t *testing.T) {
	calls := 0
	got, err := OnRateLimit(context.Background(), noopSleep, func() (repository.CreateOrderOutcome, error) {
		calls++
		return outcomeTransport(), nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.TransportError == nil {
		t.Fatalf("expected TransportError to be propagated, got nil")
	}
	if calls != 1 {
		t.Fatalf("transport error must NOT trigger retry (double-submit risk), got %d calls", calls)
	}
}

func TestOnRateLimit_DoesNotRetryOnBusinessError(t *testing.T) {
	calls := 0
	got, err := OnRateLimit(context.Background(), noopSleep, func() (repository.CreateOrderOutcome, error) {
		calls++
		return outcomeBusinessError(), nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.HTTPStatus != 400 {
		t.Fatalf("expected business-error outcome to propagate, got status %d", got.HTTPStatus)
	}
	if calls != 1 {
		t.Fatalf("non-20010 error must NOT trigger retry, got %d calls", calls)
	}
}

func TestOnRateLimit_ExhaustsRetries(t *testing.T) {
	calls := 0
	got, err := OnRateLimit(context.Background(), noopSleep, func() (repository.CreateOrderOutcome, error) {
		calls++
		return outcome20010(), nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.HTTPStatus != 500 {
		t.Fatalf("expected final 20010 outcome to propagate, got status %d", got.HTTPStatus)
	}
	if calls != 1+len(Backoffs) {
		t.Fatalf("expected 1 initial + %d retries = %d calls, got %d", len(Backoffs), 1+len(Backoffs), calls)
	}
}

func TestOnRateLimit_ContextCancelStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, _ = OnRateLimit(ctx, noopSleep, func() (repository.CreateOrderOutcome, error) {
		calls++
		return outcome20010(), nil
	})
	if calls < 1 {
		t.Fatalf("expected at least one call before ctx check, got %d", calls)
	}
	if calls > 1 {
		t.Fatalf("expected no retry after ctx cancel, got %d calls", calls)
	}
}

func TestOnRateLimit_FnErrorIsPropagated(t *testing.T) {
	myErr := errors.New("marshal failed")
	calls := 0
	_, err := OnRateLimit(context.Background(), noopSleep, func() (repository.CreateOrderOutcome, error) {
		calls++
		return repository.CreateOrderOutcome{TransportError: myErr}, myErr
	})
	if !errors.Is(err, myErr) {
		t.Fatalf("expected fn error to propagate, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 call on fn-level error, got %d", calls)
	}
}

func TestBackoffsNonEmpty(t *testing.T) {
	if len(Backoffs) == 0 {
		t.Fatal("Backoffs should not be empty")
	}
	for i, d := range Backoffs {
		if d <= 0 {
			t.Fatalf("Backoffs[%d] = %v, want positive", i, d)
		}
	}
}
