package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/braintree/manners"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

var (
	GeocodingWorkQueue chan int
)

func InternalError(ctx context.Context, err error) {
	slog.With("err", err).ErrorContext(ctx, "Internal Error")
}

type Env struct {
	database      *sql.DB
	configuration *Configuration
	metrics       *Metrics
}

func main() {
	ctx := context.Background()

	err := run(ctx, os.Args)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			slog.InfoContext(ctx, "Shutting down due to context cancellation")

			return
		}

		slog.With("err", err).ErrorContext(ctx, "Fatal error running application")
		os.Exit(1)
	}
}

var errInvalidConfig = errors.New("invalid configuration")

//nolint:funlen
func run(ctx context.Context, _ []string) error {
	ctx, cancelFunc := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancelFunc()

	// Database time
	configuration, err := getConfiguration()
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Unable to parse config")

		return errInvalidConfig
	}

	env := &Env{database: nil, configuration: configuration, metrics: NewMetrics()}

	if env.configuration.Debug {
		slog.SetDefault(
			slog.New(slog.NewTextHandler(
				os.Stdout,
				&slog.HandlerOptions{Level: slog.LevelDebug},
			)),
		)
		slog.With("config", env.configuration).
			DebugContext(ctx, "Setting debug logger.level")
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(
			os.Stdout,
			&slog.HandlerOptions{Level: slog.LevelInfo},
		)))
	}

	if env.configuration.DbHost != "" {
		env.setupDatabase(ctx)

		GeocodingWorkQueue = make(chan int)
		go env.UpdateLocationWithGeocoding(ctx, GeocodingWorkQueue)

		if env.configuration.EnableGeocodingCrawler {
			go env.GeocodingCrawler(ctx)
		}

		env.DoDatabaseMigrations(ctx)

		go func() {
			err := env.SubscribeMQTT(ctx)
			if err != nil {
				slog.With("err", err).ErrorContext(ctx, "Can't connect to MQTT")
			}
		}()
	} else {
		slog.ErrorContext(ctx, "No database host specified, disabling")

		return errInvalidConfig
	}
	defer env.closeDatabase(ctx)

	// Get the router
	if env.configuration.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()
	env.BuildRoutes(configuration, router)
	slog.With("httpPort", env.configuration.Port).
		InfoContext(ctx, "Listening on HTTP")

	err = manners.ListenAndServe(fmt.Sprintf(":%d", env.configuration.Port), router)
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Error starting server")
	}

	return nil
}

func (env *Env) closeDatabase(ctx context.Context) {
	func() {
		slog.InfoContext(ctx, "Closing database")

		if env.database != nil {
			err := env.database.Close()
			if err != nil {
				slog.With("err", err).ErrorContext(ctx, "Error closing database")
			}
		}
	}()
}

func (env *Env) setupDatabase(ctx context.Context) {
	connectionString := fmt.Sprintf(
		"host=%s user=%s dbname=%s sslmode=%s password=%s",
		env.configuration.DbHost,
		env.configuration.DbUser,
		env.configuration.DbName,
		env.configuration.DbSslMode,
		env.configuration.DbPassword,
	)

	database, err := sql.Open("postgres", connectionString)
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Error connecting to database")
	}

	err = database.Ping()
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Error connecting to database")
	} else {
		slog.InfoContext(ctx, "Database connected")
	}

	database.SetMaxOpenConns(env.configuration.MaxDBOpenConnections)
	database.SetMaxIdleConns(1)
	database.SetConnMaxLifetime(time.Hour)
	env.database = database
}
