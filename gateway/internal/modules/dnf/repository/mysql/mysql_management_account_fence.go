package mysql

import (
	"context"
	"database/sql"
)

// Every offline asset transaction takes this lock before reading a receipt or
// player state. Game admission joins this row before loading its session cache.
func lockManagedAccount(ctx context.Context, tx *sql.Tx, r mysqlRouter, account string) error {
	table, err := r.writeTable(mysqlAccountTable, account)
	if err != nil {
		return err
	}
	var actual string
	return tx.QueryRowContext(ctx, "SELECT account_id FROM "+table+" WHERE account_id=? AND BINARY account_id=BINARY ? FOR UPDATE", account, account).Scan(&actual)
}
