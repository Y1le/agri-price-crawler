package craw

import (
	"github.com/Y1le/agri-price-crawler/internal/craw/config"
)

// Run runs the craw microservice: start a small health endpoint and the scheduled price crawler.
// We avoid starting the full GenericCRAWServer here to prevent port/certificate conflicts.
func Run(cfg *config.Config) error {
	crawServer, err := createCRAWServer(cfg)
	if err != nil {
		return err
	}
	return crawServer.PrepareRun().Run()
}
