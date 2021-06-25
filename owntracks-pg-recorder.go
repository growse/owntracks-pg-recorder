package main

import (
	"database/sql"
	"fmt"
	"github.com/braintree/manners"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	GeocodingWorkQueue chan int
)

func InternalError(err error) {
	log.WithError(err).Error("Internal Error")
}

type Env struct {
	db            *sql.DB
	configuration *Configuration
}

func main() {
	//Catch SIGINT & SIGTERM to stop the profiling
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	quit := make(chan bool, 1)

	go func() {
		for sig := range c {
			log.WithField("signal", sig).Info("captured signal. Exiting...")
			if quit != nil {
				close(quit)
			}
			if GeocodingWorkQueue != nil {
				close(GeocodingWorkQueue)
			}
			log.Info("Closing manners")
			manners.Close()
		}
		log.Info("Quitting signal listener goroutine.")
	}()

	// Database time
	env := &Env{db: nil, configuration: getConfiguration()}

	if env.configuration.Debug {
		log.SetLevel(log.DebugLevel)
		log.Debug("Setting debug log level")
	} else {
		log.SetLevel(log.InfoLevel)
	}
	if env.configuration.DbHost != "" {
		env.setupDatabase(env.configuration.DbHost, env.configuration.DbUser, env.configuration.DbName)
		GeocodingWorkQueue = make(chan int, 100)
		go env.UpdateLocationWithGeocoding(GeocodingWorkQueue)
		if env.configuration.EnableGeocodingCrawler {
			go env.GeocodingCrawler(quit)
		}
		go env.SubscribeMQTT(quit)
		env.DoDatabaseMigrations(env.configuration.DatabaseMigrationsPath)
	} else {
		log.Fatal("No database host specified, disabling")
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
	log.WithField("httpPort", env.configuration.Port).Info("Listening on HTTP")
	err := manners.ListenAndServe(fmt.Sprintf(":%d", env.configuration.Port), router)
	if err != nil {
		log.WithError(err).Fatal("Error starting server")
	}
}

func (env *Env) closeDatabase() {
	func() {
		log.Info("Closing database")
		if env.db != nil {
			err := env.db.Close()
			if err != nil {
				log.WithError(err).Fatal("Error closing database")
			}
		}
	}()
}

func (env *Env) setupDatabase(host string, user string, name string) {
	connectionString := fmt.Sprintf("host=%s user=%s dbname=%s sslmode=disable", host, user, name)
	if env.configuration.DbPassword != "" {
		connectionString = fmt.Sprintf("host=%s user=%s dbname=%s sslmode=disable password=%s", env.configuration.DbHost, env.configuration.DbUser, env.configuration.DbName, env.configuration.DbPassword)
	}

	db, err := sql.Open("postgres", connectionString)

	if err != nil {
		log.WithError(err).Fatal("Error connecting to database")
	}

	err = db.Ping()
	if err != nil {
		log.WithError(err).Fatal("Error connecting to database")
	} else {
		log.Info("Database connected")
	}

	db.SetMaxOpenConns(env.configuration.MaxDBOpenConnections)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)
	env.db = db
}
