package ai

type RecipeRequest struct {
	UserID        uint64
	FavoriteFoods []string
	DislikeFoods  []string
	PriceData     map[string]float64
	Portions      int
}

type RecipeResponse struct {
	Content   string
	IsDefault bool
}
