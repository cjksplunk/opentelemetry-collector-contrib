// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sqlserverreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/sqlserverreceiver"

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/microsoft/go-mssqldb/msdsn"
)

const defaultSQLServerPort = 1433

// isLocalhost checks if the given host is a local address
func isLocalhost(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

// computeServiceInstanceID computes the service.instance.id based on the configuration
// Format: <host>:<port>
// Special handling:
// - localhost/127.0.0.1 are replaced with os.Hostname()
// - Port 0 defaults to 1433
func computeServiceInstanceID(cfg *Config) (string, error) {
	var host string
	var port int

	// Parse connection details based on configuration priority
	switch {
	case cfg.DataSource != "":
		h, p, err := parseDataSource(cfg.DataSource)
		if err != nil {
			return "", fmt.Errorf("failed to parse datasource: %w", err)
		}
		host, port = h, p
	case cfg.Server != "":
		host, port = cfg.Server, int(cfg.Port)
	default:
		// No server specified, use hostname with default port
		hostname, err := os.Hostname()
		if err != nil {
			return "", err
		}
		host, port = hostname, defaultSQLServerPort
	}

	// Replace localhost with actual hostname
	if isLocalhost(host) || host == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return "", err
		}
		host = hostname
	}

	// Apply default port if not specified
	if port == 0 {
		port = defaultSQLServerPort
	}

	return fmt.Sprintf("%s:%d", host, port), nil
}

// parseDataSource extracts server and port from SQL Server connection string
// Uses the microsoft/go-mssqldb library's built-in parser for accurate parsing
// Falls back to manual extraction if the Host field is not set by the parser
func parseDataSource(dataSource string) (string, int, error) {
	if dataSource == "" {
		return "", 0, errors.New("datasource is empty")
	}

	// Parse the connection string using the go-mssqldb library
	config, err := msdsn.Parse(dataSource)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse datasource: %w", err)
	}

	host := config.Host

	// Fallback: if Host is not set by the parser, extract it manually
	if host == "" {
		host = extractHostFromDataSource(dataSource)
	}

	// Apply default port if not specified
	port := int(config.Port)
	if port == 0 {
		port = defaultSQLServerPort
	}

	return host, port, nil
}

// extractHostFromDataSource manually extracts the host/server from various DSN formats
// Handles ADO-style, ODBC-style, and URL-style connection strings
func extractHostFromDataSource(dataSource string) string {
	// Try URL format first (sqlserver://user:password@host:port)
	if strings.Contains(dataSource, "://") {
		parts := strings.SplitN(dataSource, "://", 2)
		if len(parts) == 2 {
			// Extract authority part (everything before next /)
			authority := parts[1]
			if idx := strings.Index(authority, "/"); idx != -1 {
				authority = authority[:idx]
			}
			// Remove user:password@ prefix
			if idx := strings.LastIndex(authority, "@"); idx != -1 {
				authority = authority[idx+1:]
			}
			// Remove port suffix if present
			if idx := strings.LastIndex(authority, ":"); idx != -1 {
				// Check if what follows is a port (digits)
				potential := authority[idx+1:]
				if len(potential) > 0 && isNumeric(potential) {
					authority = authority[:idx]
				}
			}
			if authority != "" {
				return authority
			}
		}
	}

	// Try ADO/ODBC format (key=value;key=value)
	// Look for 'server', 'data source', 'address', or 'network address' keys
	parts := strings.Split(dataSource, ";")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(strings.ToLower(kv[0]))
			value := strings.TrimSpace(kv[1])

			// Remove quotes if present
			if len(value) >= 2 && strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
				value = value[1 : len(value)-1]
			}

			if key == "server" || key == "data source" || key == "address" || key == "network address" {
				// Extract just the hostname (without port)
				if idx := strings.LastIndex(value, ":"); idx != -1 {
					// Check if what follows is a port (digits)
					potential := value[idx+1:]
					if len(potential) > 0 && isNumeric(potential) {
						value = value[:idx]
					}
				}
				if value != "" {
					return value
				}
			}
		}
	}

	return ""
}

// isNumeric checks if a string contains only digits
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
