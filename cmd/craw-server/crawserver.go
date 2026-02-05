package main

import (
	"math/rand"
	"time"

	_ "go.uber.org/automaxprocs"

	_ "go.uber.org/automaxprocs"

	"github.com/Y1le/agri-price-crawler/internal/craw"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	craw.NewApp("craw").Run()
}
