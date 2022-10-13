package main

import (
	"embed"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	log "github.com/sirupsen/logrus"
)

//go:embed databasemigrations/*.sql
var migrationsFs embed.FS

func (env *Env) DoDatabaseMigrations(migrationsPath string) {
	log.Infof("Starting Database Migrations from %v", env.configuration.DatabaseMigrationsPath)
	driver, err := postgres.WithInstance(env.db, &postgres.Config{MigrationsTable: "migrations"})

	if err != nil {
		log.WithError(err).Fatal("Errors encountered creating migration driver")
	}

	sourceDriver, err := iofs.New(migrationsFs, "databasemigrations")
	if err != nil {
		log.WithError(err).Fatal("Could not create migrations source driver")
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, env.configuration.DbName, driver)

	if err != nil {
		log.WithError(err).Fatal("Errors encountered creating migrate instance")
	}
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		log.WithError(err).Fatal("Errors encountered migrating database")
	}
	log.Info("Database migration done")
}
