package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ClaudeClient はAnthropic Claude APIを使ったLLMClient実装。
type ClaudeClient struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
}

func NewClaudeClient(apiKey string, model string, maxTokens int64) *ClaudeClient {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	return &ClaudeClient{
		client:    anthropic.NewClient(opts...),
		model:     anthropic.Model(model),
		maxTokens: maxTokens,
	}
}

const systemPrompt = `You are a cryptocurrency trading strategy advisor.
Analyze the given market data and return a JSON object with your strategic recommendation.

Response format (JSON only, no other text):
{
  "stance": "TREND_FOLLOW" | "CONTRARIAN" | "HOLD",
  "reasoning": "Brief explanation of why you chose this stance"
}

Rules:
- TREND_FOLLOW: When there is a clear directional trend (up or down). The system will follow the trend using moving average crossovers.
- CONTRARIAN: When the market appears overextended (overbought/oversold). The system will look for reversal opportunities using RSI extremes.
- HOLD: When the market is unclear, choppy, or too risky to trade.
- Be conservative. When in doubt, choose HOLD.
- Consider volatility: high volatility with no clear direction → HOLD.`

// AnalyzeMarket はClaude APIに相場データを送り、戦略方針を取得する。
func (c *ClaudeClient) AnalyzeMarket(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error) {
	userMsg := buildUserMessage(marketCtx)

	message, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMsg)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("claude API error: %w", err)
	}

	if len(message.Content) == 0 {
		return nil, fmt.Errorf("claude returned empty content")
	}

	responseText := message.Content[0].Text

	var parsed struct {
		Stance    string `json:"stance"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(responseText), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse claude response: %w (raw: %s)", err, responseText)
	}

	stance := entity.MarketStance(parsed.Stance)
	switch stance {
	case entity.MarketStanceTrendFollow, entity.MarketStanceContrarian, entity.MarketStanceHold:
		// valid
	default:
		stance = entity.MarketStanceHold
	}

	return &entity.StrategyAdvice{
		Stance:    stance,
		Reasoning: parsed.Reasoning,
	}, nil
}

func buildUserMessage(mc entity.MarketContext) string {
	msg := fmt.Sprintf("Symbol ID: %d\nCurrent Price: %.2f\n\nIndicators:\n", mc.SymbolID, mc.LastPrice)

	if mc.Indicators.SMA20 != nil {
		msg += fmt.Sprintf("- SMA20: %.2f\n", *mc.Indicators.SMA20)
	}
	if mc.Indicators.SMA50 != nil {
		msg += fmt.Sprintf("- SMA50: %.2f\n", *mc.Indicators.SMA50)
	}
	if mc.Indicators.EMA12 != nil {
		msg += fmt.Sprintf("- EMA12: %.2f\n", *mc.Indicators.EMA12)
	}
	if mc.Indicators.EMA26 != nil {
		msg += fmt.Sprintf("- EMA26: %.2f\n", *mc.Indicators.EMA26)
	}
	if mc.Indicators.RSI14 != nil {
		msg += fmt.Sprintf("- RSI14: %.2f\n", *mc.Indicators.RSI14)
	}
	if mc.Indicators.MACDLine != nil {
		msg += fmt.Sprintf("- MACD Line: %.6f\n", *mc.Indicators.MACDLine)
	}
	if mc.Indicators.SignalLine != nil {
		msg += fmt.Sprintf("- Signal Line: %.6f\n", *mc.Indicators.SignalLine)
	}
	if mc.Indicators.Histogram != nil {
		msg += fmt.Sprintf("- Histogram: %.6f\n", *mc.Indicators.Histogram)
	}

	msg += "\nWhat is your strategic recommendation?"
	return msg
}
