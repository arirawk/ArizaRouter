// Package store exposes the persistence backends from internal/store to
// embedders of the SDK. ArizaRouter keeps engine config, OAuth accounts and
// cooldown state in Postgres next to the shop tables, so the embedding binary
// needs to construct the Postgres store itself.
package store

import (
	"context"

	internalstore "github.com/router-for-me/CLIProxyAPI/v7/internal/store"
)

// PostgresStoreConfig configures NewPostgresStore.
type PostgresStoreConfig = internalstore.PostgresStoreConfig

// PostgresStore mirrors config.yaml and auth JSON blobs into Postgres tables
// while serving the file watcher from a local spool directory.
type PostgresStore = internalstore.PostgresStore

// NewPostgresStore opens the Postgres-backed store and ensures its schema.
func NewPostgresStore(ctx context.Context, cfg PostgresStoreConfig) (*PostgresStore, error) {
	return internalstore.NewPostgresStore(ctx, cfg)
}
