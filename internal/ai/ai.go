package ai

var client Factory

// llm client factory
type Factory interface {
	RecipeGenerator() RecipeFactory
}

func Client() Factory {
	return client
}

func SetClient(factory Factory) {
	client = factory
}
