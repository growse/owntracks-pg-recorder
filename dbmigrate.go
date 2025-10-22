package main

import (
	"context"
	"embed"
	"errors"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed databasemigrations/*.sql
var migrationsFs embed.FS

func (env *Env) DoDatabaseMigrations() {
	ctx := context.Background()
	slog.InfoContext(ctx, "Starting Database Migrations")

	driver, err := postgres.WithInstance(env.db, &postgres.Config{MigrationsTable: "migrations"})
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Errors encountered creating migration driver")
		panic(err)
	}

	sourceDriver, err := iofs.New(migrationsFs, "databasemigrations")
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Could not create migrations source driver")
		panic(err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, env.configuration.DbName, driver)
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Errors encountered creating migrate instance")
		panic(err)
	}

	err = m.Up()

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		slog.With("err", err).ErrorContext(ctx, "Errors encountered migrating database")
		panic(err)
	}

	slog.InfoContext(ctx, "Database migration done")
}
