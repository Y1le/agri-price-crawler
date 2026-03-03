package ai

import (
	"testing"
)

func TestClientAndSetClient(t *testing.T) {
	// Test default client is nil
	originalClient := Client()
	if originalClient != nil {
		t.Errorf("Expected initial client to be nil, got %v", originalClient)
	}

	// Test setting client
	testFactory := &testFactory{}
	SetClient(testFactory)

	client := Client()
	if client != testFactory {
		t.Errorf("Expected client to be testFactory, got %v", client)
	}

	// Reset
	SetClient(nil)
}

type testFactory struct{}

func (f *testFactory) RecipeGenerator() RecipeFactory {
	return nil
}

func TestRecipeRequestStruct(t *testing.T) {
	req := &RecipeRequest{
		UserID:        123,
		FavoriteFoods: []string{"apple", "banana"},
		DislikeFoods:  []string{"spicy"},
		Portions:      2,
	}

	if req.UserID != 123 {
		t.Errorf("Expected UserID 123, got %d", req.UserID)
	}
	if len(req.FavoriteFoods) != 2 {
		t.Errorf("Expected 2 favorite foods, got %d", len(req.FavoriteFoods))
	}
	if len(req.DislikeFoods) != 1 {
		t.Errorf("Expected 1 dislike food, got %d", len(req.DislikeFoods))
	}
	if req.Portions != 2 {
		t.Errorf("Expected Portions 2, got %d", req.Portions)
	}
}

func TestRecipeResponseStruct(t *testing.T) {
	resp := &RecipeResponse{
		Content:   "Test recipe content",
		IsDefault: true,
	}

	if resp.Content != "Test recipe content" {
		t.Errorf("Expected Content 'Test recipe content', got '%s'", resp.Content)
	}
	if !resp.IsDefault {
		t.Errorf("Expected IsDefault to be true, got false")
	}

	// Test with IsDefault = false
	resp.IsDefault = false
	if resp.IsDefault {
		t.Errorf("Expected IsDefault to be false, got true")
	}
}

func TestFoodStruct(t *testing.T) {
	food := &Food{
		Name:  "Tomato",
		Price: 3.5,
		Unit:  "kg",
	}

	if food.Name != "Tomato" {
		t.Errorf("Expected Name 'Tomato', got '%s'", food.Name)
	}
	if food.Price != 3.5 {
		t.Errorf("Expected Price 3.5, got %f", food.Price)
	}
	if food.Unit != "kg" {
		t.Errorf("Expected Unit 'kg', got '%s'", food.Unit)
	}
}

func TestRecipeDataStruct(t *testing.T) {
	recipe := &RecipeData{
		RecipeName: "Tomato Stir Fry",
		Ingredients: []Ingredient{
			{Name: "Tomato", Usage: "200g", Price: 3.5, Unit: "g"},
			{Name: "Oil", Usage: "1 tbsp", Price: 0.5, Unit: " tbsp"},
		},
		Steps:     []string{"Cut tomato", "Heat oil", "Stir fry"},
		PriceTips: "Buy when in season",
	}

	if recipe.RecipeName != "Tomato Stir Fry" {
		t.Errorf("Expected RecipeName 'Tomato Stir Fry', got '%s'", recipe.RecipeName)
	}
	if len(recipe.Ingredients) != 2 {
		t.Errorf("Expected 2 ingredients, got %d", len(recipe.Ingredients))
	}
	if len(recipe.Steps) != 3 {
		t.Errorf("Expected 3 steps, got %d", len(recipe.Steps))
	}
	if recipe.PriceTips != "Buy when in season" {
		t.Errorf("Expected PriceTips 'Buy when in season', got '%s'", recipe.PriceTips)
	}
}

func TestIngredientStruct(t *testing.T) {
	ing := &Ingredient{
		Name:  "Tomato",
		Usage: "200g",
		Price: 3.5,
		Unit:  "g",
	}

	if ing.Name != "Tomato" {
		t.Errorf("Expected Name 'Tomato', got '%s'", ing.Name)
	}
	if ing.Usage != "200g" {
		t.Errorf("Expected Usage '200g', got '%s'", ing.Usage)
	}
	if ing.Price != 3.5 {
		t.Errorf("Expected Price 3.5, got %f", ing.Price)
	}
	if ing.Unit != "g" {
		t.Errorf("Expected Unit 'g', got '%s'", ing.Unit)
	}
}

func TestRecipeListStruct(t *testing.T) {
	list := &RecipeList{
		Recipes: []RecipeData{
			{RecipeName: "Recipe 1"},
			{RecipeName: "Recipe 2"},
		},
	}

	if len(list.Recipes) != 2 {
		t.Errorf("Expected 2 recipes, got %d", len(list.Recipes))
	}
}

func TestRecipeRequestWithEmptyFields(t *testing.T) {
	req := &RecipeRequest{}

	if req.UserID != 0 {
		t.Errorf("Expected default UserID 0, got %d", req.UserID)
	}
	if len(req.FavoriteFoods) != 0 {
		t.Errorf("Expected empty FavoriteFoods, got %d items", len(req.FavoriteFoods))
	}
	if len(req.DislikeFoods) != 0 {
		t.Errorf("Expected empty DislikeFoods, got %d items", len(req.DislikeFoods))
	}
	if req.Portions != 0 {
		t.Errorf("Expected default Portions 0, got %d", req.Portions)
	}
}

func TestRecipeResponseWithEmptyContent(t *testing.T) {
	resp := &RecipeResponse{}

	if resp.Content != "" {
		t.Errorf("Expected empty Content, got '%s'", resp.Content)
	}
	if resp.IsDefault {
		t.Errorf("Expected default IsDefault false, got true")
	}
}
