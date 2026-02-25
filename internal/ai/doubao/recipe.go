package doubao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/ai"
	"github.com/Y1le/agri-price-crawler/pkg/log"

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

var (
	recipeTemplate      *template.Template
	recipeEmptyTemplate *template.Template
)

func init() {
	// init recipe template
	var err error
	templatePath := filepath.Join("templates", "recipe.html")
	recipeTemplate, err = template.ParseFiles(templatePath)
	if err != nil {
		log.Fatalf("parse recipe template failed: %v", err)
	}

	emptyTemplatePath := filepath.Join("templates", "recipe_empty.html")
	recipeEmptyTemplate, err = template.ParseFiles(emptyTemplatePath)
	if err != nil {
		log.Fatalf("parse empty recipe template failed: %v", err)
	}
}

// GenerateRecipe 实现 ai.RecipeFactory 接口：生成个性化菜谱
func (c *RecipeClient) GenerateRecipe(ctx context.Context, req *ai.RecipeRequest) (*ai.RecipeResponse, error) {
	prompt := buildRecipePrompt(req)
	if len(req.PriceData) == 0 {
		fmt.Printf("price data is empty, use default recipe\n")
		emptyHTML, err := renderEmptyRecipeHTML()
		if err != nil {
			return nil, fmt.Errorf("render empty recipe template failed: %w", err)
		}
		return &ai.RecipeResponse{
			Content:   emptyHTML,
			IsDefault: true,
		}, nil
	}
	var recipeList ai.RecipeList
	err := retry.Do(
		func() error {
			reqCtx, cancel := context.WithTimeout(ctx, c.timeout) // AI只需返回JSON，超时缩短到30秒
			defer cancel()

			// 调用豆包API获取JSON数据
			resp, err := c.client.CreateChatCompletion(reqCtx, openai.ChatCompletionRequest{
				Model: c.model,
				Messages: []openai.ChatCompletionMessage{
					{Role: openai.ChatMessageRoleUser, Content: prompt},
				},
				Temperature: 0.7,
				MaxTokens:   3000, // JSON数据量小，减少Token消耗
			})
			if err != nil {
				return fmt.Errorf("doubao API call failed: %w", err)
			}

			if len(resp.Choices) == 0 {
				return errors.New("empty response from doubao")
			}

			// 提取AI返回的JSON字符串
			jsonStr := strings.TrimSpace(resp.Choices[0].Message.Content)
			// 清理可能的多余字符（如AI返回的代码块标记）
			jsonStr = strings.TrimPrefix(jsonStr, "```json")
			jsonStr = strings.TrimSuffix(jsonStr, "```")
			jsonStr = strings.TrimSpace(jsonStr)

			// 解析JSON为结构化数据
			if err := json.Unmarshal([]byte(jsonStr), &recipeList); err != nil {
				return fmt.Errorf("parse doubao JSON response failed: %w, raw response: %s", err, jsonStr)
			}

			if len(recipeList.Recipes) < 1 {
				return fmt.Errorf("invalid recipe count: %d (expected at least 1)", len(recipeList.Recipes))
			}
			return nil
		},
		retry.Attempts(uint(c.maxRetries)),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxDelay(5*time.Second),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			log.Warnf("doubao retry %d: %v", n+1, err)
		}),
	)

	if err != nil {
		fmt.Printf("doubao call failed after retries: %v, use default recipe\n", err)
		return &ai.RecipeResponse{
			Content:   getDefaultRecipe(req), // 默认菜谱
			IsDefault: true,
		}, nil
	}

	// 渲染结构化数据为HTML
	htmlContent, err := renderRecipeHTML(&recipeList)
	if err != nil {
		fmt.Printf("doubao call failed after retries: %v, use default recipe\n", err)
		return &ai.RecipeResponse{
			Content:   getDefaultRecipe(req),
			IsDefault: true,
		}, nil
	}

	return &ai.RecipeResponse{
		Content:   htmlContent,
		IsDefault: false,
	}, nil
}

// buildRecipePrompt 构造豆包 Prompt（要求返回结构化 JSON）
func buildRecipePrompt(req *ai.RecipeRequest) string {
	var sb strings.Builder
	sb.WriteString(`请严格按照以下 JSON 格式返回【3道】不同类型的个性化家庭菜谱数据，仅返回 JSON 字符串，不要添加任何多余文字、解释或格式说明：
	{
		"recipes": [
			{
			"recipe_name": "菜谱名称1（如：番茄炒蛋）",
			"ingredients": [
				{"name": "食材名", "usage": "用量", "price": 单价数字, "unit": "单位"}
			],
			"steps": ["步骤1", "步骤2"],
			"price_tips": "性价比建议1"
			},
			{
			"recipe_name": "菜谱名称2（如：青椒土豆丝）",
			"ingredients": [{"name": "食材名", "usage": "用量", "price": 单价数字, "unit": "单位"}],
			"steps": ["步骤1", "步骤2"],
			"price_tips": "性价比建议2"
			},
			{
			"recipe_name": "菜谱名称3（如：清炒西兰花）",
			"ingredients": [{"name": "食材名", "usage": "用量", "price": 单价数字, "unit": "单位"}],
			"steps": ["步骤1", "步骤2"],
			"price_tips": "性价比建议3"
			}
		]
	}

	=== 生成依据 ===
	1. 用户食材偏好：` + strings.Join(req.FavoriteFoods, "、") + `
	2. 用户不喜欢的食材：` + strings.Join(req.DislikeFoods, "、") + `
	3. 今日食材价格：
	`)
	for _, food := range req.PriceData {
		sb.WriteString(fmt.Sprintf("   - %s：%.2f 元/%s\n", food.Name, food.Price, food.Unit))
	}
	sb.WriteString(`=== 生成要求 ===
	1. 菜谱名称简洁易懂，符合家庭烹饪场景；
	2. 食材用量具体，烹饪步骤清晰（3-5步）；
	3. 价格Tips结合价格数据，优先推荐性价比高的搭配；
	4. 严格遵守JSON格式，字段名和类型必须匹配，不能缺失字段；
	5. 避免使用专业烹饪术语，语言亲切。`)
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

// renderRecipeHTML 将结构化数据渲染为HTML
func renderRecipeHTML(data *ai.RecipeList) (string, error) {
	log.Debugf("renderRecipeHTML: %v", data)
	if recipeTemplate == nil {
		return "", errors.New("recipe template not initialized")
	}

	temp := recipeTemplate

	// 使用缓冲区渲染模板
	var buf strings.Builder
	err := temp.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("render template failed: %w", err)
	}

	return buf.String(), nil
}

func renderEmptyRecipeHTML() (string, error) {
	if recipeEmptyTemplate == nil {
		return "", errors.New("empty recipe template not initialized")
	}

	var buf strings.Builder
	// 空模板无需传参，直接渲染
	err := recipeEmptyTemplate.Execute(&buf, nil)
	if err != nil {
		return "", fmt.Errorf("render empty template failed: %w", err)
	}

	return buf.String(), nil
}
