package main

import (
	"database/sql"
	"fmt"
	"github.com/braintree/manners"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	GeocodingWorkQueue chan bool
)

func InternalError(err error) {
	log.Printf("%v", err)
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
			log.Printf("captured %v. Exiting...", sig)
			if quit != nil {
				close(quit)
			}
			if GeocodingWorkQueue != nil {
				close(GeocodingWorkQueue)
			}
			log.Print("Closing manners")
			manners.Close()
		}
		log.Print("Quitting signal listener goroutine.")
	}()

	// Database time
	env := &Env{db: nil, configuration: getConfiguration()}
	if env.configuration.DbHost != "" {
		env.setupDatabase(env.configuration.DbHost, env.configuration.DbUser, env.configuration.DbName)
		GeocodingWorkQueue = make(chan bool, 100)
		go env.UpdateLatestLocationWithGeocoding(GeocodingWorkQueue)
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
	//if env.configuration.Production {
	gin.SetMode(gin.ReleaseMode)
	//} else {
	//	gin.SetMode(gin.DebugMode)
	//}
	router := gin.Default()
	env.BuildRoutes(router)
	log.Printf("Listening on port %d", env.configuration.Port)
	err := manners.ListenAndServe(fmt.Sprintf(":%d", env.configuration.Port), router)
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

func (env *Env) closeDatabase() {
	func() {
		log.Println("Closing database")
		if env.db != nil {
			err := env.db.Close()
			if err != nil {
				log.Fatalf("Error closing database: %v", err)
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
		log.Fatalf("Error connecting to database: %v", err)
	}

	err = db.Ping()
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	} else {
		log.Print("Database connected")
	}

	log.Printf("Setting maximum db connections to %d", env.configuration.MaxDBOpenConnections)
	db.SetMaxOpenConns(env.configuration.MaxDBOpenConnections)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)
	env.db = db
}
