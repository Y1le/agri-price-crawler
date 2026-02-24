package doubao

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/ai"

	"github.com/avast/retry-go/v4"
	"github.com/sashabaranov/go-openai"
)

type RecipeClient struct {
	client     *openai.Client
	timeout    time.Duration
	maxRetries int
	model      string
}

var _ ai.RecipeFactory = (*RecipeClient)(nil)

// GenerateRecipe 实现 ai.RecipeFactory 接口：生成个性化菜谱
func (c *RecipeClient) GenerateRecipe(ctx context.Context, req *ai.RecipeRequest) (*ai.RecipeResponse, error) {
	// 构造豆包 Prompt
	prompt := buildRecipePrompt(req)

	// 带重试的 API 调用
	var recipeContent string
	err := retry.Do(
		func() error {
			// 设置单次请求超时（叠加外层 ctx 超时）
			reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()

			// 调用豆包 API（兼容 openai 协议）
			resp, err := c.client.CreateChatCompletion(reqCtx, openai.ChatCompletionRequest{
				Model: c.model,
				Messages: []openai.ChatCompletionMessage{
					{
						Role:    openai.ChatMessageRoleUser,
						Content: prompt,
					},
				},
				Temperature: 0.7, // 生成多样性
				MaxTokens:   800, // 最大生成长度
				TopP:        1.0,
			})
			if err != nil {
				return fmt.Errorf("doubao api call failed: %w", err)
			}

			if len(resp.Choices) == 0 {
				return errors.New("empty response from doubao")
			}

			recipeContent = resp.Choices[0].Message.Content
			return nil
		},
		// 指数退避重试配置
		retry.Attempts(uint(c.maxRetries)),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxDelay(5*time.Second),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			fmt.Printf("doubao retry %d: %v\n", n+1, err)
		}),
	)

	// 失败降级（返回默认菜谱）
	if err != nil {
		fmt.Printf("doubao call failed after retries: %v, use default recipe\n", err)
		return &ai.RecipeResponse{
			Content:   getDefaultRecipe(req), // 默认菜谱
			IsDefault: true,
		}, nil
	}

	// 成功返回
	return &ai.RecipeResponse{
		Content:   recipeContent,
		IsDefault: false,
	}, nil
}

// buildRecipePrompt 构造豆包 Prompt
func buildRecipePrompt(req *ai.RecipeRequest) string {
	var sb strings.Builder
	sb.WriteString("请根据以下信息生成一份个性化家庭菜谱：\n")
	sb.WriteString("=== 基础信息 ===\n")
	sb.WriteString(fmt.Sprintf("1. 用户食材偏好：%s\n", strings.Join(req.FavoriteFoods, "、")))
	sb.WriteString(fmt.Sprintf("2. 用户不喜欢的食材：%s\n", strings.Join(req.DislikeFoods, "、")))
	sb.WriteString("2. 今日食材价格：\n")
	for ing, price := range req.PriceData {
		sb.WriteString(fmt.Sprintf("   - %s：%.2f 元/斤\n", ing, price))
	}
	sb.WriteString(fmt.Sprintf("3. 用餐份数：%d 人份\n", req.Portions))
	sb.WriteString("=== 生成要求 ===\n")
	sb.WriteString("1. 菜谱名称简洁易懂，符合家庭烹饪场景；\n")
	sb.WriteString("2. 包含详细的食材用量、烹饪步骤；\n")
	sb.WriteString("3. 结合价格数据，优先推荐性价比高的搭配；\n")
	sb.WriteString("4. 语言风格亲切，步骤清晰，无专业术语。")
	return sb.String()
}

// getDefaultRecipe 降级用的默认提示文案（服务繁忙场景）
func getDefaultRecipe(req *ai.RecipeRequest) string {
	var sb strings.Builder
	// 核心提示文案
	sb.WriteString("【温馨提示】\n")
	sb.WriteString("当前菜谱生成服务繁忙，请您稍后重试～\n")
	// 反馈引导
	sb.WriteString("如果该问题持续出现，可联系我们的客服反馈，感谢您的理解！\n")
	// 保留极简的请求标识（便于排查问题，可选）
	sb.WriteString(fmt.Sprintf("请求ID参考：%d\n", req.UserID))
	return sb.String()
}
