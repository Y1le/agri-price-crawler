package store

var client Factory

// PriceDataStorage defines the PriceData storage interface.
type Factory interface {
	Prices() PriceStorage
	Close() error
}

func Client() Factory {
	return client
}

func SetClient(factory Factory) {
	client = factory
}
