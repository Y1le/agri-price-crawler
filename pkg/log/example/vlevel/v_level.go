package main

import (
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

func main() {
	defer log.Flush()

	log.V(0).Info("This is a V level message")
	log.V(0).Infow("This is a V level message with fields", "X-Request-ID", "7a7b9f24-4cae-4b2a-9464-69088b45b904")
}
