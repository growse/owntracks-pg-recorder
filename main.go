package main

import (
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
	slog.Error("Internal Error", "err", err)
}

type Env struct {
	db            *sql.DB
	configuration *Configuration
	metrics       *Metrics
}

func main() {
	//Catch SIGINT & SIGTERM to stop the profiling
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	quit := make(chan bool, 1)

	go func() {
		for sig := range c {
			slog.Info("captured signal. Exiting...", "signal", sig)
			if quit != nil {
				close(quit)
			}
			if GeocodingWorkQueue != nil {
				close(GeocodingWorkQueue)
			}
			slog.Info("Closing manners")
			manners.Close()
		}
		slog.Info("Quitting signal listener goroutine.")
	}()

	// Database time
	configuration, err := getConfiguration()
	if err != nil {
		slog.Error("Unable to parse config", "err", err)
		os.Exit(1)
	}
	env := &Env{db: nil, configuration: configuration, metrics: NewMetrics()}

	if env.configuration.Debug {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
		slog.Debug("Setting debug logger.level", "config", env.configuration)
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
				slog.Error("Can't connect to MQTT", "err", err)
			}
		}()

	} else {
		slog.Error("No database host specified, disabling")
		os.Exit(1)
	}
	defer env.closeDatabase()

	//Get the router
	if env.configuration.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.Default()
	env.BuildRoutes(router)
	slog.Info("Listening on HTTP", "httpPort", env.configuration.Port)
	err = manners.ListenAndServe(fmt.Sprintf(":%d", env.configuration.Port), router)
	if err != nil {
		slog.Error("Error starting server", "err", err)
	}
}

func (env *Env) closeDatabase() {
	func() {
		slog.Info("Closing database")
		if env.db != nil {
			err := env.db.Close()
			if err != nil {
				slog.Error("Error closing database", "err", err)
			}
		}
	}()
}

func (env *Env) setupDatabase() {
	connectionString := fmt.Sprintf("host=%s user=%s dbname=%s sslmode=%s password=%s", env.configuration.DbHost, env.configuration.DbUser, env.configuration.DbName, env.configuration.DbSslMode, env.configuration.DbPassword)

	db, err := sql.Open("postgres", connectionString)

	if err != nil {
		slog.Error("Error connecting to database", "err", err)
	}

	err = db.Ping()
	if err != nil {
		slog.Error("Error connecting to database", "err", err)
	} else {
		slog.Info("Database connected")
	}

	db.SetMaxOpenConns(env.configuration.MaxDBOpenConnections)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)
	env.db = db
}
