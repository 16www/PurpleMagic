package management

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/repository/mysql"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

// InitializeSchema is an explicit installation step. It never migrates game tables.
func InitializeSchema(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := sql.Open("mysql", cfg.RepositoryDSN)
	if err != nil {
		return errors.New("management database configuration invalid")
	}
	defer data.Close()
	options := mysql.MySQLGroupOptions{TablePrefix: cfg.TablePrefix, DatabasePlan: repository.DatabasePlan{WriteDatabases: cfg.Databases, ReadDatabases: cfg.Databases}}
	if err = mysql.EnsureManagementSchema(ctx, data, options); err != nil {
		return errors.New("management receipt schema initialization failed")
	}
	registration, err := sql.Open("mysql", cfg.RegistrationDSN)
	if err != nil {
		return errors.New("registration database configuration invalid")
	}
	defer registration.Close()
	store, err := testbenefits.NewStore(registration, cfg.AccountTable)
	if err != nil {
		return err
	}
	if err = store.EnsureSchema(ctx); err != nil {
		return errors.New("management benefit schema initialization failed")
	}
	return nil
}
