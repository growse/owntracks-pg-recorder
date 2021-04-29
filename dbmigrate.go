package main

import (
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"log"
)

func (env *Env) DoDatabaseMigrations(migrationsPath string) {
	log.Printf("Starting Database Migrations from %v", env.configuration.DatabaseMigrationsPath)
	driver, err := postgres.WithInstance(env.db, &postgres.Config{MigrationsTable: "migrations"})

	if err != nil {
		log.Fatalf("Errors encountered creating migration driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, env.configuration.DbName, driver)

	if err != nil {
		log.Fatalf("Errors encountered creating migrate instance : %v", err)
	}
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		log.Fatalf("Errors encountered migrating database: %v", err)
	}
	log.Print("Database migration done")
}
