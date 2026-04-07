package sqlitecli

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

func init() {
	sql.Register("sqlitecli", &sqliteDriver{})
}

type sqliteDriver struct{}

type sqliteConn struct {
	databaseFile string
}

type sqliteRows struct {
	columns []string
	rows    [][]string
	index   int
}

func (driverInstance *sqliteDriver) Open(databaseFile string) (driver.Conn, error) {
	if strings.TrimSpace(databaseFile) == "" {
		return nil, errors.New("database file is required")
	}
	return &sqliteConn{databaseFile: databaseFile}, nil
}

func (connection *sqliteConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not supported")
}

func (connection *sqliteConn) Close() error { return nil }

func (connection *sqliteConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}

func (connection *sqliteConn) ExecContext(_ context.Context, query string, namedArguments []driver.NamedValue) (driver.Result, error) {
	statement, err := applyNamedArguments(query, namedArguments)
	if err != nil {
		return nil, err
	}

	if _, err := runSQLite(connection.databaseFile, statement, false); err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}

func (connection *sqliteConn) QueryContext(_ context.Context, query string, namedArguments []driver.NamedValue) (driver.Rows, error) {
	statement, err := applyNamedArguments(query, namedArguments)
	if err != nil {
		return nil, err
	}

	rawOutput, err := runSQLite(connection.databaseFile, statement, true)
	if err != nil {
		return nil, err
	}
	return buildRowsFromOutput(rawOutput), nil
}

func runSQLite(databaseFile string, statement string, includeHeader bool) (string, error) {
	args := []string{}
	if includeHeader {
		args = append(args, "-header")
	}
	args = append(args, "-separator", "\x1f", databaseFile, statement)

	command := exec.Command("sqlite3", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sqlite command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func applyNamedArguments(query string, namedArguments []driver.NamedValue) (string, error) {
	result := query
	for _, namedArgument := range namedArguments {
		replacement, err := sqlLiteral(namedArgument.Value)
		if err != nil {
			return "", err
		}
		result = strings.Replace(result, "?", replacement, 1)
	}
	if strings.Contains(result, "?") {
		return "", errors.New("not all placeholders were replaced")
	}
	return result, nil
}

func sqlLiteral(argumentValue any) (string, error) {
	switch convertedValue := argumentValue.(type) {
	case nil:
		return "NULL", nil
	case int64:
		return strconv.FormatInt(convertedValue, 10), nil
	case float64:
		return strconv.FormatFloat(convertedValue, 'f', -1, 64), nil
	case bool:
		if convertedValue {
			return "1", nil
		}
		return "0", nil
	case []byte:
		return "'" + strings.ReplaceAll(string(convertedValue), "'", "''") + "'", nil
	case string:
		return "'" + strings.ReplaceAll(convertedValue, "'", "''") + "'", nil
	default:
		return "", fmt.Errorf("unsupported argument type %T", argumentValue)
	}
}

func buildRowsFromOutput(rawOutput string) *sqliteRows {
	trimmedOutput := strings.TrimSpace(rawOutput)
	if trimmedOutput == "" {
		return &sqliteRows{}
	}

	lines := strings.Split(trimmedOutput, "\n")
	if len(lines) == 0 {
		return &sqliteRows{}
	}

	columns := strings.Split(lines[0], "\x1f")
	dataRows := make([][]string, 0, len(lines)-1)
	for _, singleLine := range lines[1:] {
		dataRows = append(dataRows, strings.Split(singleLine, "\x1f"))
	}
	return &sqliteRows{columns: columns, rows: dataRows}
}

func (rows *sqliteRows) Columns() []string {
	return rows.columns
}

func (rows *sqliteRows) Close() error { return nil }

func (rows *sqliteRows) Next(destination []driver.Value) error {
	if rows.index >= len(rows.rows) {
		return io.EOF
	}

	currentRow := rows.rows[rows.index]
	for columnIndex := range destination {
		if columnIndex < len(currentRow) {
			destination[columnIndex] = currentRow[columnIndex]
			continue
		}
		destination[columnIndex] = nil
	}
	rows.index++
	return nil
}
