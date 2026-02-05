package craw

import (
	"github.com/Y1le/agri-price-crawler/internal/craw/config"
	"github.com/Y1le/agri-price-crawler/internal/craw/options"
	"github.com/Y1le/agri-price-crawler/pkg/app"
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

func NewApp(basename string) *app.App {
	opts := options.NewOptions()
	application := app.NewApp("Agri Price Crawler",
		basename,
		app.WithOptions(opts),
		app.WithDefaultValidArgs(),
		app.WithRunFunc(run(opts)),
	)

	return application
}

func run(opts *options.Options) app.RunFunc {
	return func(basename string) error {
		log.Init(opts.Log)
		defer log.Flush()

		cfg, err := config.CreateConfigFromOptions(opts)
		if err != nil {
			return err
		}

		return Run(cfg)
	}
}
