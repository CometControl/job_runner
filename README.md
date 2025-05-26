# Job Runner

A lightweight server that executes SQL queries on-demand via HTTP requests and returns the results as metrics in Prometheus format.

## Overview

Job Runner is a complete refactoring of the SQL Metrics Server (which itself was a refactoring of the SQL Exporter for Prometheus). It accepts query parameters in HTTP GET requests to perform SQL queries and return the results as metrics.

Key features:
- On-demand SQL query execution via HTTP GET requests
- Support for multiple database types (PostgreSQL, MySQL, Oracle, SQL Server, SQLite)
- Simple configuration via JSON config file or command-line flags
- Uses VictoriaMetrics for efficient metrics generation
- Modular package-based structure for easier maintenance

## Usage

### Starting the server

```
./job_runner --config=config.json
```

Or with command-line overrides:

```
./job_runner --config=config.json --http.addr=127.0.0.1 --http.port=9090
```

### Configuration

The server can be configured using a JSON configuration file:

```json
{
  "http_addr": "0.0.0.0",
  "http_port": 8080,
  "connection_options": {
    "max_connections": 10,
    "max_idle_connections": 5,
    "max_connection_lifetime": "10m",
    "disable_ssl": false,
    "connect_timeout": "10s",
    "query_timeout": "30s",
    "prepared_statements": true,
    "no_ping": false
  },
  "query_metric_name": "sql_query_result",
  "query_status_metric_name": "sql_query_status"
}
```

### Making requests

To query a database and get metrics, make a GET request to the `/sql` endpoint with the following parameters:

| Parameter | Description | Required |
|-----------|-------------|----------|
| `query` | SQL query to execute | Yes |
| `type` | Database type (pg, mysql, oracle, sqlserver, sqlite) | Yes |
| `username` | Database username | Yes (except for SQLite) |
| `password` | Database password | Yes (except for SQLite) |
| `host` | Database host | Yes (except for SQLite) |
| `port` | Database port | No (defaults to standard port for the database type) |
| `db` | Database name or file path for SQLite | Yes |
| `value_column` | Column to use as metric value | No (default: "value") |
| `metric_prefix` | Prefix for metric names | No (default: "sql_query_result" from config) |

Example:

```
http://localhost:8080/sql?type=pg&username=user&password=pass&host=localhost&db=postgres&query=SELECT+name,+value+FROM+metrics&value_column=value
```

For SQLite:

```
http://localhost:8080/sql?type=sqlite&db=C:/path/to/database.db&query=SELECT+name,+value+FROM+metrics&value_column=value
```

### Query Structure

The query should return:
1. One column that will be used as the metric value (specified by `value_column`)
2. Any number of additional columns that will be used as labels

For example:

```sql
SELECT 
  database_name, 
  schema_name, 
  table_name, 
  row_count as value 
FROM 
  table_stats
```

This would create metrics like:

```
sql_query_result{database_name="mydb",schema_name="public",table_name="users"} 1250
sql_query_result{database_name="mydb",schema_name="public",table_name="orders"} 5432
```

## Testing

The Job Runner includes a comprehensive test suite that uses SQLite for local testing. To run the tests:

```
cd cmd/job_runner
./test.ps1  # On Windows
./test.sh   # On Linux/macOS
```

The tests include:
- Unit tests for individual packages
- Integration tests for the HTTP server
- Example SQLite database with test data

## Security Considerations

**Warning**: This application accepts database credentials as query parameters, which poses security risks:
- Credentials may be logged in server logs
- Credentials appear in URLs which might be stored in browser history
- URLs with credentials might be exposed to third parties via the Referer header

For production use, consider:
1. Running this server behind a reverse proxy with authentication
2. Restricting access to trusted networks only
3. Using database users with minimal privileges

## Building from Source

### Using Go

```powershell
# On Windows
cd cmd/job_runner
./build.ps1
```

```bash
# On Linux/macOS
cd cmd/job_runner
./build.sh
```

### Using Docker

```powershell
# Build the Docker image
docker build -t job-runner .

# Run the container
docker run -p 8080:8080 -v ${PWD}/config.json:/app/config.json job-runner
```

### Using Docker Compose

```powershell
# Start the service
docker-compose up -d

# View logs
docker-compose logs -f

# Stop the service
docker-compose down
```

## Project Structure

```
job_runner/
├── cmd/               # Command-line applications
│   └── job_runner/    # Main application
├── config/            # Configuration handling
├── db/                # Database connection and query execution
├── docs/              # Documentation
├── errors/            # Error types and handling
├── examples/          # Example scripts
├── metric/            # Metric generation
├── server/            # HTTP server implementation
└── tests/             # Testing utilities
```
