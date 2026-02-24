package doubao

import (
	"errors"
	"sync"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/ai"
	genericoptions "github.com/Y1le/agri-price-crawler/internal/pkg/options"
	"github.com/sashabaranov/go-openai"
)

// DoubaoFactory 豆包客户端工厂（实现 ai.Factory 接口）
type dataFactory struct {
	client  *openai.Client
	Options genericoptions.DoubaoOptions
}

func (db *dataFactory) RecipeGenerator() ai.RecipeFactory {
	return newRecipeGenerator(db)
}

var _ ai.Factory = (*dataFactory)(nil)

var (
	doubaoFactory ai.Factory
	once          sync.Once
)

func GetDoubaoFactoryOr(opts *genericoptions.DoubaoOptions) (ai.Factory, error) {
	if opts == nil && doubaoFactory == nil {
		return nil, errors.New("failed to get doubao factory options")
	}
	once.Do(func() {
		config := openai.DefaultConfig(opts.APIKey)
		config.BaseURL = opts.BaseURL
		doubaoFactory = &dataFactory{
			client:  openai.NewClientWithConfig(config),
			Options: *opts,
		}
	})
	if doubaoFactory == nil {
		return nil, errors.New("doubao factory is nil")
	}
	return doubaoFactory, nil
}

func newRecipeGenerator(f *dataFactory) *RecipeClient {
	return &RecipeClient{
		client:     f.client,
		timeout:    time.Duration(f.Options.TimeoutSec) * time.Second,
		maxRetries: f.Options.MaxRetries,
		model:      f.Options.Model,
	}
}
