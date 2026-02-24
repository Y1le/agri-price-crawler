package ai

import "context"

type RecipeFactory interface {
	GenerateRecipe(ctx context.Context, req *RecipeRequest) (*RecipeResponse, error)
}
