package collectors

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/example/mysql-monitor-connector/internal/mysqltarget"
)

type Definition struct {
	Query     string
	Arguments func(json.RawMessage) ([]any, error)
}

var catalogue = map[string]Definition{
	"collect.server_identity.v1": {
		Query:     `SELECT VERSION() AS version, @@hostname AS hostname, @@port AS port, @@read_only AS read_only`,
		Arguments: noParameters,
	},
	"collect.global_status.v1": {
		Query:     `SHOW GLOBAL STATUS`,
		Arguments: noParameters,
	},
	"collect.global_variables.v1": {
		Query:     `SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_variables ORDER BY VARIABLE_NAME`,
		Arguments: noParameters,
	},
	"collect.statement_digests.v1": {
		Query:     `SELECT SCHEMA_NAME, DIGEST, DIGEST_TEXT, COUNT_STAR, SUM_TIMER_WAIT, SUM_ROWS_EXAMINED, SUM_ROWS_SENT FROM performance_schema.events_statements_summary_by_digest WHERE DIGEST IS NOT NULL ORDER BY SUM_TIMER_WAIT DESC LIMIT ?`,
		Arguments: limitParameter,
	},
	"collect.table_io.v1": {
		Query:     `SELECT table_schema, table_name, rows_fetched, rows_inserted, rows_updated, rows_deleted, io_read_requests, io_write_requests FROM sys.schema_table_statistics ORDER BY total_latency DESC LIMIT ?`,
		Arguments: limitParameter,
	},
	"collect.lock_waits.v1": {
		Query:     `SELECT wait_started, wait_age_secs, locked_table_schema, locked_table_name, waiting_pid, blocking_pid FROM sys.innodb_lock_waits ORDER BY wait_age_secs DESC LIMIT ?`,
		Arguments: limitParameter,
	},
}

func Execute(ctx context.Context, db *sql.DB, operation string, parameters json.RawMessage, maxRows, maxBytes int) (mysqltarget.QueryResult, error) {
	definition, ok := catalogue[operation]
	if !ok {
		return mysqltarget.QueryResult{}, errors.New("operation is not in the local collector catalogue")
	}
	args, err := definition.Arguments(parameters)
	if err != nil {
		return mysqltarget.QueryResult{}, err
	}
	return mysqltarget.Query(ctx, db, definition.Query, args, maxRows, maxBytes)
}

func Known(operation string) bool { _, ok := catalogue[operation]; return ok }

func noParameters(raw json.RawMessage) ([]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("{}")) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	return nil, errors.New("operation does not accept parameters")
}

func limitParameter(raw json.RawMessage) ([]any, error) {
	var p struct {
		Limit int `json:"limit"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(bytes.TrimSpace(raw)) == 0 {
		p.Limit = 100
	} else if err := decoder.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if p.Limit == 0 {
		p.Limit = 100
	}
	if p.Limit < 1 || p.Limit > 1000 {
		return nil, errors.New("limit must be between 1 and 1000")
	}
	return []any{p.Limit}, nil
}
