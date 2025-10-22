package main

import (
	"context"
	"database/sql"
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

func InternalError(err error) {
	slog.With("err", err).ErrorContext(context.Background(), "Internal Error")
}

type Env struct {
	db            *sql.DB
	configuration *Configuration
	metrics       *Metrics
}

func main() {
	ctx := context.Background()
	// Catch SIGINT & SIGTERM to stop the profiling
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	quit := make(chan bool, 1)

	go func() {
		for sig := range c {
			slog.With("signal", sig).InfoContext(ctx, "captured signal. Exiting...")

			if quit != nil {
				close(quit)
			}

			if GeocodingWorkQueue != nil {
				close(GeocodingWorkQueue)
			}

			slog.InfoContext(ctx, "Closing manners")
			manners.Close()
		}

		slog.InfoContext(ctx, "Quitting signal listener goroutine.")
	}()

	// Database time
	configuration, err := getConfiguration()
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Unable to parse config")
		os.Exit(1)
	}

	env := &Env{db: nil, configuration: configuration, metrics: NewMetrics()}

	if env.configuration.Debug {
		slog.SetDefault(
			slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})),
		)
		slog.With("config", env.configuration).DebugContext(ctx, "Setting debug logger.level")
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}

	if env.configuration.DbHost != "" {
		env.setupDatabase()

		GeocodingWorkQueue = make(chan int, 100)
		go env.UpdateLocationWithGeocoding(GeocodingWorkQueue)

		if env.configuration.EnableGeocodingCrawler {
			go env.GeocodingCrawler(quit)
		}

		env.DoDatabaseMigrations()

		go func() {
			err := env.SubscribeMQTT(quit)
			if err != nil {
				slog.With("err", err).ErrorContext(ctx, "Can't connect to MQTT")
			}
		}()
	} else {
		slog.ErrorContext(ctx, "No database host specified, disabling")
		os.Exit(1)
	}
	defer env.closeDatabase()

	// Get the router
	if env.configuration.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()
	env.BuildRoutes(configuration, router)
	slog.With("httpPort", env.configuration.Port).InfoContext(ctx, "Listening on HTTP")

	err = manners.ListenAndServe(fmt.Sprintf(":%d", env.configuration.Port), router)
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Error starting server")
	}
}

func (env *Env) closeDatabase() {
	ctx := context.Background()

	func() {
		slog.InfoContext(ctx, "Closing database")

		if env.db != nil {
			err := env.db.Close()
			if err != nil {
				slog.With("err", err).ErrorContext(ctx, "Error closing database")
			}
		}
	}()
}

func (env *Env) setupDatabase() {
	ctx := context.Background()
	connectionString := fmt.Sprintf(
		"host=%s user=%s dbname=%s sslmode=%s password=%s",
		env.configuration.DbHost,
		env.configuration.DbUser,
		env.configuration.DbName,
		env.configuration.DbSslMode,
		env.configuration.DbPassword,
	)

	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Error connecting to database")
	}

	err = db.Ping()
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Error connecting to database")
	} else {
		slog.InfoContext(ctx, "Database connected")
	}

	db.SetMaxOpenConns(env.configuration.MaxDBOpenConnections)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)
	env.db = db
}
