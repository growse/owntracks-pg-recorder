package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

var (
	GeocodingWorkQueue   chan int
	DawarichForwardQueue chan MQTTMsg
)

func InternalError(ctx context.Context, err error) {
	slog.With("err", err).ErrorContext(ctx, "Internal Error")
}

type Env struct {
	database      *sql.DB
	configuration *Configuration
	metrics       *Metrics
	insertSem     chan struct{} // bounds concurrent DB inserts
	tmpl          *template.Template
}

func main() {
	ctx := context.Background()

	if len(os.Args) > 1 && os.Args[1] == "sync-dawarich" {
		ctx, cancelFunc := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer cancelFunc()
		if err := runSyncDawarich(ctx, os.Args[2:]); err != nil {
			slog.With("err", err).ErrorContext(ctx, "Sync failed")
			os.Exit(1)
		}
		return
	}

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

	env := &Env{
		database:      nil,
		configuration: configuration,
		metrics:       NewMetrics(),
		insertSem:     make(chan struct{}, configuration.MaxDBOpenConnections),
	}

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
		err := env.setupDatabase(ctx)
		if err != nil {
			return fmt.Errorf("database setup failed: %w", err)
		}

		GeocodingWorkQueue = make(chan int, 100)

		go func() {
			<-ctx.Done()
			close(GeocodingWorkQueue)
		}()
		go env.UpdateLocationWithGeocoding(ctx, GeocodingWorkQueue)

		if env.configuration.EnableGeocodingCrawler {
			go env.GeocodingCrawler(ctx)
		}

		if env.configuration.DawarichURL != "" {
			DawarichForwardQueue = make(chan MQTTMsg, 100)

			go func() {
				<-ctx.Done()
				close(DawarichForwardQueue)
			}()

			go env.ForwardToDawarich(ctx, DawarichForwardQueue)
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
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", env.configuration.Port),
		Handler: env.BuildRoutes(configuration),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		slog.InfoContext(shutdownCtx, "Shutting down HTTP server")

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.With("err", err).ErrorContext(shutdownCtx, "Error shutting down HTTP server")
		}
	}()

	slog.With("httpPort", env.configuration.Port).
		InfoContext(ctx, "Listening on HTTP")

	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.With("err", err).ErrorContext(ctx, "Error starting server")

		return err
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

func (env *Env) setupDatabase(ctx context.Context) error {
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
		return fmt.Errorf("error opening database connection: %w", err)
	}

	err = database.Ping()
	if err != nil {
		return fmt.Errorf("error pinging database: %w", err)
	}

	slog.InfoContext(ctx, "Database connected")

	database.SetMaxOpenConns(env.configuration.MaxDBOpenConnections)
	database.SetMaxIdleConns(env.configuration.MaxDBOpenConnections)
	database.SetConnMaxLifetime(time.Hour)
	env.database = database

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				s := database.Stats()
				slog.DebugContext(ctx, "db pool stats",
					"open", s.OpenConnections,
					"in_use", s.InUse,
					"idle", s.Idle,
					"wait_count", s.WaitCount,
					"wait_duration", s.WaitDuration,
				)
			}
		}
	}()

	return nil
}

func runSyncDawarich(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sync-dawarich", flag.ExitOnError)
	startFlag := fs.String("start", "", "Start time in RFC3339 format (optional)")
	endFlag := fs.String("end", "", "End time in RFC3339 format (optional)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	configuration, err := getConfiguration()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	if configuration.DawarichURL == "" {
		return errors.New("DawarichURL is not set in configuration")
	}

	if configuration.Debug {
		slog.SetDefault(
			slog.New(slog.NewTextHandler(
				os.Stdout,
				&slog.HandlerOptions{Level: slog.LevelDebug},
			)),
		)
	}

	env := &Env{
		configuration: configuration,
		metrics:       NewMetrics(),
	}

	if err := env.setupDatabase(ctx); err != nil {
		return fmt.Errorf("database setup failed: %w", err)
	}

	defer env.closeDatabase(ctx)

	var start time.Time

	if *startFlag != "" {
		start, err = time.Parse(time.RFC3339, *startFlag)
		if err != nil {
			return fmt.Errorf("parsing --start: %w", err)
		}
	}

	end := time.Now()

	if *endFlag != "" {
		end, err = time.Parse(time.RFC3339, *endFlag)
		if err != nil {
			return fmt.Errorf("parsing --end: %w", err)
		}
	}

	return env.SyncToDawarich(ctx, start, end)
}
