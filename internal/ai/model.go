package ai

type RecipeRequest struct {
	UserID        uint64
	FavoriteFoods []string
	DislikeFoods  []string
	PriceData     []Food
	Portions      int
}

type RecipeResponse struct {
	Content   string
	IsDefault bool
}

// Food 食材结构体
type Food struct {
	Name  string
	Price float64
	Unit  string
}

type RecipeList struct {
	Recipes []RecipeData `json:"recipes"`
}

// RecipeData AI 返回的结构化菜谱数据（与 Prompt 要求的字段对应）
type RecipeData struct {
	RecipeName  string       `json:"recipe_name"` // 菜谱名称
	Ingredients []Ingredient `json:"ingredients"` // 食材用量
	Steps       []string     `json:"steps"`       // 烹饪步骤
	PriceTips   string       `json:"price_tips"`  // 性价比建议
}

// Ingredient 食材用量结构体
type Ingredient struct {
	Name  string  `json:"name"`  // 食材名称
	Usage string  `json:"usage"` // 用量（如：200克、1勺）
	Price float64 `json:"price"` // 食材单价（元/单位）
	Unit  string  `json:"unit"`  // 单位（克、斤、勺）
}
