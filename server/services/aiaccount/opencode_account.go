package aiaccount

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	_ "github.com/glebarez/go-sqlite"
)

// readOpenCodeActiveAccounts observes stable V2 account IDs without reading
// token values or mistaking ordinary session writes for credential changes.
func readOpenCodeActiveAccounts(ctx context.Context, path string) ([]string, error) {
	if path == ":memory:" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, status_error.AIAccountCredentialError
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	source := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(500)"}
	db, err := sql.Open("sqlite", source.String())
	if err != nil {
		return nil, status_error.AIAccountCredentialError
	}
	defer db.Close()
	var table string
	err = db.QueryRowContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name = 'credential'`).Scan(&table)
	if errors.Is(err, sql.ErrNoRows) {
		// V1 stores its credentials in the separate auth.json file.
		return nil, nil
	}
	if err != nil {
		return nil, status_error.AIAccountCredentialError
	}
	const maxActiveAccounts = 256
	rows, err := db.QueryContext(ctx, `SELECT connector_id, id FROM credential WHERE active = 1 ORDER BY connector_id LIMIT ?`, maxActiveAccounts+1)
	if err != nil {
		return nil, status_error.AIAccountCredentialError
	}
	defer rows.Close()
	var accounts []string
	for rows.Next() {
		var connector, id string
		if err := rows.Scan(&connector, &id); err != nil || len(accounts) >= 2*maxActiveAccounts {
			return nil, status_error.AIAccountCredentialError
		}
		accounts = append(accounts, connector, id)
	}
	if rows.Err() != nil {
		return nil, status_error.AIAccountCredentialError
	}
	return accounts, nil
}
