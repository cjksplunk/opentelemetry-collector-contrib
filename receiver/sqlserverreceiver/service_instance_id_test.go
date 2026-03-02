// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sqlserverreceiver

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/configopaque"
)

func getTestHostname() string {
	hostname, _ := os.Hostname()
	return hostname
}

func TestComputeServiceInstanceID(t *testing.T) {
	hostname := getTestHostname()

	tests := []struct {
		name     string
		config   *Config
		expected string
		wantErr  bool
	}{
		{
			name: "explicit server and port",
			config: &Config{
				Server: "myserver",
				Port:   5000,
			},
			expected: "myserver:5000",
		},
		{
			name: "explicit server default port zero",
			config: &Config{
				Server: "myserver",
				Port:   0,
			},
			expected: "myserver:1433",
		},
		{
			name: "explicit server default port 1433",
			config: &Config{
				Server: "myserver",
				Port:   1433,
			},
			expected: "myserver:1433",
		},
		{
			name: "datasource with port comma separator",
			config: &Config{
				DataSource: "server=myserver,5000;user id=sa;password=pass",
			},
			expected: "myserver:5000",
		},
		{
			name: "datasource with port colon separator",
			config: &Config{
				DataSource: "server=myserver:5000;user id=sa;password=pass",
			},
			expected: "myserver:5000:1433", // msdsn treats "myserver:5000" as the host name
		},
		{
			name: "datasource without port",
			config: &Config{
				DataSource: "server=myserver;user id=sa;password=pass",
			},
			expected: "myserver:1433",
		},
		{
			name: "datasource with data source keyword",
			config: &Config{
				DataSource: "Data Source=myserver,5000;Initial Catalog=mydb",
			},
			expected: "myserver:5000",
		},
		{
			name: "datasource with separate port param",
			config: &Config{
				DataSource: "server=myserver;port=5000;user id=sa",
			},
			expected: "myserver:5000",
		},
		{
			name: "localhost replacement with explicit server",
			config: &Config{
				Server: "localhost",
				Port:   1433,
			},
			expected: fmt.Sprintf("%s:1433", hostname),
		},
		{
			name: "localhost 127.0.0.1 replacement",
			config: &Config{
				Server: "127.0.0.1",
				Port:   1433,
			},
			expected: fmt.Sprintf("%s:1433", hostname),
		},
		{
			name: "localhost in datasource",
			config: &Config{
				DataSource: "server=localhost;user id=sa",
			},
			expected: fmt.Sprintf("%s:1433", hostname),
		},
		{
			name: "empty server in datasource",
			config: &Config{
				DataSource: "user id=sa",
			},
			expected: fmt.Sprintf("%s:1433", hostname),
		},
		{
			name:     "no server uses hostname",
			config:   &Config{},
			expected: fmt.Sprintf("%s:1433", hostname),
		},
		{
			name: "no server no datasource",
			config: &Config{
				Username: "sa",
				Password: configopaque.String("pass"),
			},
			expected: fmt.Sprintf("%s:1433", hostname),
		},
		{
			name: "datasource with named instance",
			config: &Config{
				DataSource: "server=myserver\\SQLEXPRESS,5000;user id=sa",
			},
			expected: "myserver:5000", // Instance name not included in service.instance.id
		},
		{
			name: "datasource with spaces around equals",
			config: &Config{
				DataSource: "server = myserver , 5000 ; user id = sa",
			},
			wantErr: true, // msdsn cannot parse spaces around comma in port
		},
		{
			name: "case insensitive datasource keywords",
			config: &Config{
				DataSource: "SERVER=MyServer,5000;User Id=sa",
			},
			expected: "MyServer:5000",
		},
		{
			name: "invalid datasource no server defaults to localhost",
			config: &Config{
				DataSource: "user id=sa;password=pass",
			},
			expected: fmt.Sprintf("%s:1433", hostname), // msdsn defaults to localhost when no server specified
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := computeServiceInstanceID(tt.config)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, actual)
			}
		})
	}
}

func TestParseDataSource(t *testing.T) {
	tests := []struct {
		name         string
		dataSource   string
		expectedHost string
		expectedPort int
		wantErr      bool
	}{
		{
			name:         "standard comma separator",
			dataSource:   "server=myserver,5000;user id=sa",
			expectedHost: "myserver",
			expectedPort: 5000,
		},
		{
			name:         "colon separator treated as host",
			dataSource:   "server=myserver:5000;user id=sa",
			expectedHost: "myserver:5000", // msdsn treats this as host
			expectedPort: 1433,
		},
		{
			name:         "no port specified",
			dataSource:   "server=myserver;user id=sa",
			expectedHost: "myserver",
			expectedPort: 1433,
		},
		{
			name:         "separate port parameter",
			dataSource:   "server=myserver;port=5000;user id=sa",
			expectedHost: "myserver",
			expectedPort: 5000,
		},
		{
			name:         "data source keyword",
			dataSource:   "Data Source=myserver,5000",
			expectedHost: "myserver",
			expectedPort: 5000,
		},
		{
			name:         "named instance",
			dataSource:   "server=myserver\\SQLEXPRESS,5000",
			expectedHost: "myserver", // Instance name not included
			expectedPort: 5000,
		},
		{
			name:       "spaces in connection string",
			dataSource: "server = myserver , 5000 ; user id = sa",
			wantErr:    true, // msdsn cannot parse spaces around comma
		},
		{
			name:         "preserve case of server name",
			dataSource:   "Server=MyServerName,5000",
			expectedHost: "MyServerName",
			expectedPort: 5000,
		},
		{
			name:         "no server in datasource defaults to localhost",
			dataSource:   "user id=sa;password=pass",
			expectedHost: "localhost", // msdsn defaults to localhost
			expectedPort: 1433,
		},
		{
			name:       "empty datasource",
			dataSource: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := parseDataSource(tt.dataSource)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedHost, host)
				assert.Equal(t, tt.expectedPort, port)
			}
		})
	}
}

// TestIsLocalhost tests the isLocalhost function
func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected bool
	}{
		{
			name:     "localhost lowercase",
			host:     "localhost",
			expected: true,
		},
		{
			name:     "localhost uppercase",
			host:     "LOCALHOST",
			expected: true,
		},
		{
			name:     "localhost mixed case",
			host:     "LocalHost",
			expected: true,
		},
		{
			name:     "127.0.0.1",
			host:     "127.0.0.1",
			expected: true,
		},
		{
			name:     "127.0.0.2",
			host:     "127.0.0.2",
			expected: true,
		},
		{
			name:     "::1 (IPv6 loopback)",
			host:     "::1",
			expected: true,
		},
		{
			name:     "remote host",
			host:     "sqlserver.example.com",
			expected: false,
		},
		{
			name:     "IP address",
			host:     "192.168.1.1",
			expected: false,
		},
		{
			name:     "empty string",
			host:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLocalhost(tt.host)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsNumeric tests the isNumeric helper function
func TestIsNumeric(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid port number",
			input:    "1433",
			expected: true,
		},
		{
			name:     "zero",
			input:    "0",
			expected: true,
		},
		{
			name:     "large number",
			input:    "65535",
			expected: true,
		},
		{
			name:     "with letters",
			input:    "1433abc",
			expected: false,
		},
		{
			name:     "with special characters",
			input:    "14-33",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "space",
			input:    " ",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNumeric(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractHostFromDataSource tests the extractHostFromDataSource function
// This tests the fallback mechanism when msdsn.Parse() doesn't set Host properly
func TestExtractHostFromDataSource(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		expected string
	}{
		// URL format tests
		{
			name:     "URL format with host only",
			dsn:      "sqlserver://localhost",
			expected: "localhost",
		},
		{
			name:     "URL format with host and port",
			dsn:      "sqlserver://localhost:1433",
			expected: "localhost",
		},
		{
			name:     "URL format with user and password",
			dsn:      "sqlserver://sa:password@localhost",
			expected: "localhost",
		},
		{
			name:     "URL format with user, password, host, and port",
			dsn:      "sqlserver://sa:password@localhost:1433",
			expected: "localhost",
		},
		{
			name:     "URL format with IP address",
			dsn:      "sqlserver://sa:password@192.168.1.100:1433",
			expected: "192.168.1.100",
		},
		{
			name:     "URL format with database path",
			dsn:      "sqlserver://sa:password@localhost:1433/mydb",
			expected: "localhost",
		},
		{
			name:     "URL format with complex password",
			dsn:      "sqlserver://sa:p@ss%40word@sqlserver.example.com:1433",
			expected: "sqlserver.example.com",
		},
		// ADO/ODBC format tests
		{
			name:     "ADO format with server",
			dsn:      "server=localhost;user id=sa;password=password",
			expected: "localhost",
		},
		{
			name:     "ADO format with data source",
			dsn:      "data source=localhost;user id=sa;password=password",
			expected: "localhost",
		},
		{
			name:     "ADO format with address",
			dsn:      "address=localhost;user id=sa;password=password",
			expected: "localhost",
		},
		{
			name:     "ADO format with network address",
			dsn:      "network address=localhost;user id=sa;password=password",
			expected: "localhost",
		},
		{
			name:     "ADO format with server and port",
			dsn:      "server=localhost:1433;user id=sa;password=password",
			expected: "localhost",
		},
		{
			name:     "ADO format with quoted values",
			dsn:      `server="localhost";user id="sa";password="password"`,
			expected: "localhost",
		},
		{
			name:     "ADO format case insensitive",
			dsn:      "SERVER=localhost;USER ID=sa;PASSWORD=password",
			expected: "localhost",
		},
		{
			name:     "ADO format mixed case",
			dsn:      "Server=myserver.example.com;User Id=sa;Password=password",
			expected: "myserver.example.com",
		},
		{
			name:     "ADO format with IP address",
			dsn:      "server=192.168.1.100:1433;uid=sa;pwd=password",
			expected: "192.168.1.100",
		},
		{
			name:     "ADO format with spaces in keys",
			dsn:      "data source=localhost; user id=sa; password=password",
			expected: "localhost",
		},
		{
			name:     "ADO format with FQDN",
			dsn:      "Server=mydb.company.com:1500;User Id=sa;Password=password",
			expected: "mydb.company.com",
		},
		// Edge cases
		{
			name:     "empty string",
			dsn:      "",
			expected: "",
		},
		{
			name:     "only server key",
			dsn:      "server=localhost",
			expected: "localhost",
		},
		{
			name:     "no recognized server keys",
			dsn:      "user id=sa;password=password",
			expected: "",
		},
		{
			name:     "named instance with server key",
			dsn:      "server=localhost\\SQLEXPRESS",
			expected: "localhost\\SQLEXPRESS", // Fallback preserves the full server value
		},
		{
			name:     "port as comma-separated in server value",
			dsn:      "server=localhost,5000;user id=sa",
			expected: "localhost,5000", // Fallback preserves comma-separated format
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHostFromDataSource(tt.dsn)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseDataSourceFallbackMechanism tests that fallback extraction works when msdsn doesn't set Host
func TestParseDataSourceFallbackMechanism(t *testing.T) {
	tests := []struct {
		name         string
		dataSource   string
		expectedHost string
		expectedPort int
	}{
		{
			name:         "fallback when only ADO-style keys provided",
			dataSource:   "server=localhost;uid=sa",
			expectedHost: "localhost",
			expectedPort: defaultSQLServerPort,
		},
		{
			name:         "fallback with quoted server value",
			dataSource:   `server="localhost";user id="sa"`,
			expectedHost: "localhost",
			expectedPort: defaultSQLServerPort,
		},
		{
			name:         "fallback extracts port from server value",
			dataSource:   "server=localhost:1500;uid=sa",
			expectedHost: "localhost:1500",     // Fallback returns raw value; msdsn.Parse handles port extraction
			expectedPort: defaultSQLServerPort, // fallback doesn't extract from colon format
		},
		{
			name:         "fallback with FQDN",
			dataSource:   "server=db.example.com;uid=sa;pwd=pass",
			expectedHost: "db.example.com",
			expectedPort: defaultSQLServerPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := parseDataSource(tt.dataSource)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedHost, host)
			assert.Equal(t, tt.expectedPort, port)
		})
	}
}

// BenchmarkParseDataSource benchmarks the parseDataSource function
func BenchmarkParseDataSource(b *testing.B) {
	dsn := "server=localhost;user id=sa;password=password"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = parseDataSource(dsn)
	}
}

// BenchmarkExtractHostFromDataSource benchmarks the extractHostFromDataSource function
func BenchmarkExtractHostFromDataSource(b *testing.B) {
	dsn := "server=localhost;user id=sa;password=password"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractHostFromDataSource(dsn)
	}
}

// BenchmarkComputeServiceInstanceID benchmarks the computeServiceInstanceID function
func BenchmarkComputeServiceInstanceID(b *testing.B) {
	cfg := &Config{
		DataSource: "server=localhost;user id=sa;password=password",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = computeServiceInstanceID(cfg)
	}
}
