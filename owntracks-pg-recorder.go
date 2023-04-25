package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/braintree/manners"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
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
				log.Fatalf("Can't connect to MQTT: %v", err)
			}
		}()

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

func (env *Env) setupDatabase() {
	connectionString := fmt.Sprintf("host=%s user=%s dbname=%s sslmode=%s password=%s", env.configuration.DbHost, env.configuration.DbUser, env.configuration.DbName, env.configuration.DbSslMode, env.configuration.DbPassword)

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
