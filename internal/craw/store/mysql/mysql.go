package mysql

import (
	"fmt"
	"sync"

	"github.com/Y1le/agri-price-crawler/internal/craw/store"
	"github.com/Y1le/agri-price-crawler/internal/pkg/logger"
	genericoptions "github.com/Y1le/agri-price-crawler/internal/pkg/options"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/db"
	"github.com/influxdata/influxdb/kit/errors"
	"gorm.io/gorm"
)

type dataStore struct {
	db *gorm.DB
	// can include two database instance if needed
	// docker *grom.DB
	// db *gorm.DB
}

func (ds *dataStore) HNPrices() store.HNPriceStore {
	return newHNPrices(ds.db)
}

func (ds *dataStore) Users() store.UserStore {
	return newUsers(ds.db)
}
func (ds *dataStore) Subscribes() store.SubscribeStore {
	return newSubscribes(ds.db)
}

func (ds *dataStore) Close() error {
	db, err := ds.db.DB()
	if err != nil {
		return errors.Wrap(err, "failed to get database connection")
	}

	return db.Close()
}

var (
	mysqlFactory store.Factory
	once         sync.Once
)

func GetMySQLFactoryOr(opts *genericoptions.MySQLOptions) (store.Factory, error) {
	if opts == nil && mysqlFactory == nil {
		return nil, fmt.Errorf("failed to get mysql store factory")
	}
	var err error
	var dbIns *gorm.DB
	once.Do(func() {
		options := &db.Options{
			Host:                  opts.Host,
			Username:              opts.Username,
			Password:              opts.Password,
			Database:              opts.Database,
			MaxIdleConnections:    opts.MaxIdleConnections,
			MaxOpenConnections:    opts.MaxOpenConnections,
			MaxConnectionLifeTime: opts.MaxConnectionLifeTime,
			LogLevel:              opts.LogLevel,
			Logger:                logger.New(opts.LogLevel),
		}
		dbIns, err = db.New(options)

		mysqlFactory = &dataStore{db: dbIns}
	})
	if mysqlFactory == nil || err != nil {
		return nil, fmt.Errorf("failed to get mysql store fatory, mysqlFactory: %+v, error: %w", mysqlFactory, err)
	}
	return mysqlFactory, nil
}

// cleanDatabase tear downs the database tables.
//
//nolint:unused // may be reused in the feature, or just show a migrate usage.
func cleanDatabase(db *gorm.DB) error {
	if err := db.Migrator().DropTable(&v1.Price{}); err != nil {
		return errors.Wrap(err, "drop Price table failed")
	}

	return nil
}

// migrateDatabase run auto migration for given models, will only add missing fields,
// won't delete/change current data.
//
//nolint:unused // may be reused in the feature, or just show a migrate usage.
func migrateDatabase(db *gorm.DB) error {
	if err := db.AutoMigrate(&v1.Price{}); err != nil {
		return errors.Wrap(err, "migrate price model failed")
	}

	return nil
}

// resetDatabase resets the database tables.
//
//nolint:unused,deadcode // may be reused in the feature, or just show a migrate usage.
func resetDatabase(db *gorm.DB) error {
	if err := cleanDatabase(db); err != nil {
		return err
	}
	if err := migrateDatabase(db); err != nil {
		return err
	}
	return nil
}
