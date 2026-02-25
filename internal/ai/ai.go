package ai

var client Factory

// llm client factory
//
//go:generate mockgen -source=$GOFILE -destination=./mock/mock_ai.go -package=mock
type Factory interface {
	RecipeGenerator() RecipeFactory
}

func Client() Factory {
	return client
}

func SetClient(factory Factory) {
	client = factory
}
