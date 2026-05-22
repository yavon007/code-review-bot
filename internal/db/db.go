package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(10)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(30 * time.Minute)

	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func Migrate(ctx context.Context, database *sql.DB) error {
	if err := ensureMigrationTable(ctx, database); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, err := migrationVersion(entry.Name())
		if err != nil {
			return err
		}

		applied, err := isMigrationApplied(ctx, database, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		sqlBytes, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		if err := applyMigration(ctx, database, version, entry.Name(), string(sqlBytes)); err != nil {
			return err
		}
	}

	return nil
}

func ensureMigrationTable(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `
		create table if not exists schema_migrations (
			version text primary key,
			name text not null,
			applied_at timestamptz not null default now()
		)
	`)
	return err
}

func migrationVersion(name string) (string, error) {
	version, _, ok := strings.Cut(name, "_")
	if !ok || version == "" {
		return "", fmt.Errorf("invalid migration filename %q", name)
	}
	return version, nil
}

func isMigrationApplied(ctx context.Context, database *sql.DB, version string) (bool, error) {
	var applied bool
	err := database.QueryRowContext(ctx, `
		select exists(select 1 from schema_migrations where version = $1)
	`, version).Scan(&applied)
	return applied, err
}

func applyMigration(ctx context.Context, database *sql.DB, version string, name string, sqlText string) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `
		insert into schema_migrations (version, name) values ($1, $2)
	`, version, name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return tx.Commit()
}
