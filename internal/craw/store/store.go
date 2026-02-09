package store

var client Factory

// PriceDataStore defines the PriceData Store interface.
type Factory interface {
	HNPrices() HNPriceStore
	Users() UserStore
	Subscribes() SubscribeStore
	Close() error
}

func Client() Factory {
	return client
}

func SetClient(factory Factory) {
	client = factory
}
