package mysqltarget

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/example/mysql-monitor-connector/internal/config"
	mysql "github.com/go-sql-driver/mysql"
)

var registeredTLS sync.Map

func Open(target config.Target, password []byte) (*sql.DB, error) {
	if err := config.ValidateTarget(target); err != nil {
		return nil, err
	}
	driverConfig := mysql.NewConfig()
	driverConfig.User = target.User
	driverConfig.Passwd = string(password)
	driverConfig.Net = target.Network
	driverConfig.Addr = target.Address
	driverConfig.DBName = target.Database
	driverConfig.Timeout = 5 * time.Second
	driverConfig.ReadTimeout = 15 * time.Second
	driverConfig.WriteTimeout = 15 * time.Second
	driverConfig.MultiStatements = false
	driverConfig.InterpolateParams = false
	driverConfig.ParseTime = true
	driverConfig.Collation = "utf8mb4_general_ci"

	if target.Network == "tcp" {
		caPEM, err := os.ReadFile(target.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("read MySQL CA: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("MySQL CA file contains no certificates")
		}
		hash := sha256.Sum256(append(append([]byte{}, caPEM...), []byte("\x00"+target.TLSServerName)...))
		name := fmt.Sprintf("mysql-monitor-%x", hash[:12])
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
			ServerName: target.TLSServerName,
		}
		if _, loaded := registeredTLS.LoadOrStore(name, struct{}{}); !loaded {
			if err := mysql.RegisterTLSConfig(name, tlsConfig); err != nil {
				registeredTLS.Delete(name)
				return nil, fmt.Errorf("register MySQL TLS configuration: %w", err)
			}
		}
		driverConfig.TLSConfig = name
	}

	db, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	return db, nil
}

type QueryResult struct {
	Columns   []string
	Rows      [][]any
	Truncated bool
}

func Query(ctx context.Context, db *sql.DB, query string, args []any, maxRows, maxBytes int) (QueryResult, error) {
	var result QueryResult
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return result, err
	}
	result.Columns = columns
	encodedColumns, err := json.Marshal(columns)
	if err != nil {
		return result, err
	}
	usedBytes := len(encodedColumns)
	for rows.Next() {
		if len(result.Rows) >= maxRows {
			result.Truncated = true
			break
		}
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return result, err
		}
		for i, value := range values {
			switch v := value.(type) {
			case []byte:
				values[i] = string(v)
			}
		}
		encodedRow, err := json.Marshal(values)
		if err != nil {
			return result, err
		}
		rowBytes := len(encodedRow)
		if usedBytes+rowBytes > maxBytes {
			result.Truncated = true
			break
		}
		usedBytes += rowBytes
		result.Rows = append(result.Rows, values)
	}
	return result, rows.Err()
}
