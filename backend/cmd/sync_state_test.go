package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// fakeRakutenClient は repository.OrderClient の最小実装。
// GetAssets / GetPositions のみ本テストで使う。他メソッドは呼ばれないので nil で返す。
type fakeRakutenClient struct {
	positions []entity.Position

	// getAssetsSequence は GetAssets の戻り値列。呼ばれるたびに 1 要素ずつ進む。
	// シーケンスが尽きたら最後の要素を返す。
	getAssetsSequence []getAssetsResult
	getAssetsCalls    atomic.Int64
}

type getAssetsResult struct {
	assets []entity.Asset
	err    error
}

func (f *fakeRakutenClient) GetAssets(ctx context.Context) ([]entity.Asset, error) {
	idx := f.getAssetsCalls.Add(1) - 1
	if int(idx) >= len(f.getAssetsSequence) {
		idx = int64(len(f.getAssetsSequence) - 1)
	}
	r := f.getAssetsSequence[idx]
	return r.assets, r.err
}

func (f *fakeRakutenClient) GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error) {
	return f.positions, nil
}

func (f *fakeRakutenClient) CreateOrder(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
	return nil, nil
}
func (f *fakeRakutenClient) CreateOrderRaw(ctx context.Context, req entity.OrderRequest) (repository.CreateOrderOutcome, error) {
	return repository.CreateOrderOutcome{}, nil
}
func (f *fakeRakutenClient) CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error) {
	return nil, nil
}
func (f *fakeRakutenClient) GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error) {
	return nil, nil
}
func (f *fakeRakutenClient) GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error) {
	return nil, nil
}

// TestSyncState_RetriesOn20010 は syncState が GetAssets の 20010 エラーを
// 最大 3 回までリトライし、最終的に成功したらその残高を riskMgr に反映することを検証する。
func TestSyncState_RetriesOn20010(t *testing.T) {
	err20010 := fmt.Errorf("GetAssets: API error (status 500): {\"code\":20010}")

	fake := &fakeRakutenClient{
		getAssetsSequence: []getAssetsResult{
			{err: err20010},
			{assets: []entity.Asset{{Currency: "JPY", OnhandAmount: "9995"}}},
		},
	}

	riskMgr := usecase.NewRiskManager(entity.RiskConfig{InitialCapital: 10000})
	p := &TradingPipeline{
		symbolID:    7,
		restClient:  fake,
		riskMgr:     riskMgr,
		sleepFn:     func(d time.Duration) {}, // no-op sleep: テストで実時間を消費しない
	}

	p.syncState(context.Background())

	balance := riskMgr.GetStatus().Balance
	if balance != 9995 {
		t.Fatalf("expected balance to be updated to 9995 after 20010 retry, got %.2f", balance)
	}
	if fake.getAssetsCalls.Load() != 2 {
		t.Fatalf("expected GetAssets to be called 2 times (1 failure + 1 success), got %d", fake.getAssetsCalls.Load())
	}
}

// TestSyncState_NoRetryOnNon20010Error は 20010 以外のエラーではリトライしないことを
// 検証する。残高は初期値のままで、GetAssets 呼び出しは 1 回のみ。
func TestSyncState_NoRetryOnNon20010Error(t *testing.T) {
	otherErr := fmt.Errorf("GetAssets: API error (status 500): {\"code\":99999}")

	fake := &fakeRakutenClient{
		getAssetsSequence: []getAssetsResult{
			{err: otherErr},
		},
	}

	riskMgr := usecase.NewRiskManager(entity.RiskConfig{InitialCapital: 10000})
	p := &TradingPipeline{
		symbolID:   7,
		restClient: fake,
		riskMgr:    riskMgr,
		sleepFn:    func(d time.Duration) {},
	}

	p.syncState(context.Background())

	if fake.getAssetsCalls.Load() != 1 {
		t.Fatalf("expected GetAssets to be called once for non-20010 error, got %d", fake.getAssetsCalls.Load())
	}
	if balance := riskMgr.GetStatus().Balance; balance != 10000 {
		t.Fatalf("expected balance to remain at initial 10000 on non-retryable error, got %.2f", balance)
	}
}
