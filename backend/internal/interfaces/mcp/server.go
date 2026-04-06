package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// Dependencies はMCPサーバーが必要とする依存コンポーネント。
type Dependencies struct {
	RiskManager         *usecase.RiskManager
	LLMService          *usecase.LLMService
	IndicatorCalculator *usecase.IndicatorCalculator
	MarketDataService   *usecase.MarketDataService
	OrderClient         repository.OrderClient
}

// NewServer はMCPサーバーを作成する。
func NewServer(deps Dependencies) *server.MCPServer {
	s := server.NewMCPServer(
		"rakuten-trading-bot",
		"1.0.0",
	)

	addStatusTool(s, deps)
	addPnLTool(s, deps)
	addStrategyTool(s, deps)
	addIndicatorsTool(s, deps)
	addPositionsTool(s, deps)
	addConfigTool(s, deps)
	addUpdateConfigTool(s, deps)

	return s
}

func addStatusTool(s *server.MCPServer, deps Dependencies) {
	s.AddTool(
		gomcp.NewTool("get_status",
			gomcp.WithDescription("ボットの稼働状態を取得する（残高、日次損失、ポジション総額、停止状態）"),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			status := deps.RiskManager.GetStatus()
			return jsonResult(map[string]any{
				"status":          statusLabel(status),
				"tradingHalted":   status.TradingHalted,
				"manuallyStopped": status.ManuallyStopped,
				"balance":         status.Balance,
				"dailyLoss":       status.DailyLoss,
				"totalPosition":   status.TotalPosition,
			})
		},
	)
}

func addPnLTool(s *server.MCPServer, deps Dependencies) {
	s.AddTool(
		gomcp.NewTool("get_pnl",
			gomcp.WithDescription("損益情報を取得する（残高、日次損失、ポジション総額、取引停止状態）"),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			status := deps.RiskManager.GetStatus()
			return jsonResult(map[string]any{
				"balance":       status.Balance,
				"dailyLoss":     status.DailyLoss,
				"totalPosition": status.TotalPosition,
				"tradingHalted": status.TradingHalted,
			})
		},
	)
}

func addStrategyTool(s *server.MCPServer, deps Dependencies) {
	s.AddTool(
		gomcp.NewTool("get_strategy",
			gomcp.WithDescription("LLMの現在の戦略方針を取得する（TREND_FOLLOW/CONTRARIAN/HOLD）"),
			gomcp.WithNumber("symbolId",
				gomcp.Description("銘柄ID（デフォルト: 7 = BTC_JPY）"),
			),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			symbolID := int64(req.GetInt("symbolId", 7))
			advice := deps.LLMService.GetCachedAdvice(symbolID)
			if advice == nil {
				return gomcp.NewToolResultText("no cached strategy advice available"), nil
			}
			return jsonResult(advice)
		},
	)
}

func addIndicatorsTool(s *server.MCPServer, deps Dependencies) {
	s.AddTool(
		gomcp.NewTool("get_indicators",
			gomcp.WithDescription("テクニカル指標を取得する（SMA, EMA, RSI, MACD）"),
			gomcp.WithNumber("symbolId",
				gomcp.Required(),
				gomcp.Description("銘柄ID（例: 7 = BTC_JPY）"),
			),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			symbolID, err := req.RequireInt("symbolId")
			if err != nil {
				return gomcp.NewToolResultError(err.Error()), nil
			}
			indicators, err := deps.IndicatorCalculator.Calculate(ctx, int64(symbolID), "15min")
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("failed to calculate indicators: %v", err)), nil
			}
			return jsonResult(indicators)
		},
	)
}

func addPositionsTool(s *server.MCPServer, deps Dependencies) {
	s.AddTool(
		gomcp.NewTool("get_positions",
			gomcp.WithDescription("現在��ポジション一覧を取得する"),
			gomcp.WithNumber("symbolId",
				gomcp.Description("銘柄ID（デフォルト: 7 = BTC_JPY）"),
			),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			symbolID := int64(req.GetInt("symbolId", 7))
			if deps.OrderClient == nil {
				return gomcp.NewToolResultError("order client not configured"), nil
			}
			positions, err := deps.OrderClient.GetPositions(ctx, symbolID)
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("failed to get positions: %v", err)), nil
			}
			return jsonResult(positions)
		},
	)
}

func addConfigTool(s *server.MCPServer, deps Dependencies) {
	s.AddTool(
		gomcp.NewTool("get_config",
			gomcp.WithDescription("リスク管理パラメータを取得する（ポジション上限、日次損失上限、損切りライン、軍資金）"),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			status := deps.RiskManager.GetStatus()
			return jsonResult(status.Config)
		},
	)
}

func addUpdateConfigTool(s *server.MCPServer, deps Dependencies) {
	s.AddTool(
		gomcp.NewTool("update_config",
			gomcp.WithDescription("リスク管理パラメータを更新する。指定したパラメータのみ更新される。"),
			gomcp.WithNumber("maxPositionAmount",
				gomcp.Description("同時ポジション上限（円）"),
			),
			gomcp.WithNumber("maxDailyLoss",
				gomcp.Description("日次損失上限（円）"),
			),
			gomcp.WithNumber("stopLossPercent",
				gomcp.Description("損切りライン（%）"),
			),
			gomcp.WithNumber("initialCapital",
				gomcp.Description("軍資金（円）"),
			),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			status := deps.RiskManager.GetStatus()
			cfg := status.Config

			args := req.GetArguments()
			if v, ok := args["maxPositionAmount"]; ok {
				if f, ok := v.(float64); ok {
					if f <= 0 {
						return gomcp.NewToolResultError("maxPositionAmount must be positive"), nil
					}
					cfg.MaxPositionAmount = f
				}
			}
			if v, ok := args["maxDailyLoss"]; ok {
				if f, ok := v.(float64); ok {
					if f <= 0 {
						return gomcp.NewToolResultError("maxDailyLoss must be positive"), nil
					}
					cfg.MaxDailyLoss = f
				}
			}
			if v, ok := args["stopLossPercent"]; ok {
				if f, ok := v.(float64); ok {
					if f <= 0 {
						return gomcp.NewToolResultError("stopLossPercent must be positive"), nil
					}
					cfg.StopLossPercent = f
				}
			}
			if v, ok := args["initialCapital"]; ok {
				if f, ok := v.(float64); ok {
					if f <= 0 {
						return gomcp.NewToolResultError("initialCapital must be positive"), nil
					}
					cfg.InitialCapital = f
				}
			}

			deps.RiskManager.UpdateConfig(cfg)
			return jsonResult(cfg)
		},
	)
}

func jsonResult(v any) (*gomcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return gomcp.NewToolResultText(string(data)), nil
}

func statusLabel(status usecase.RiskStatus) string {
	if status.ManuallyStopped {
		return "stopped"
	}
	return "running"
}
