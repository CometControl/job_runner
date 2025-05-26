# Example script to query SQLite and get metrics

# Set SQLite database path - using the test database for this example
$SQLITE_DB = ".\tests\test_data.db"

# Set the Job Runner URL
$SERVER_URL = "http://localhost:8080/metrics"

# Check if test database exists, create it if it doesn't
if (-Not (Test-Path $SQLITE_DB)) {
    Write-Host "Creating test SQLite database..." -ForegroundColor Yellow
    
    # Create directory if it doesn't exist
    $dir = Split-Path $SQLITE_DB
    if (-Not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir | Out-Null
    }
    
    # Install SQLite if needed (using Invoke-WebRequest or chocolatey)
    # ...
    
    # Create tables
    & sqlite3 $SQLITE_DB @"
        CREATE TABLE tables (
            name TEXT PRIMARY KEY,
            rows INTEGER,
            size INTEGER
        );
        
        INSERT INTO tables (name, rows, size) VALUES 
        ('users', 1250, 5120),
        ('orders', 5432, 25600),
        ('products', 842, 3200),
        ('categories', 50, 512);
        
        CREATE TABLE metrics (
            name TEXT,
            value REAL,
            timestamp INTEGER
        );
        
        INSERT INTO metrics (name, value, timestamp) VALUES 
        ('cpu_usage', 45.2, strftime('%s','now')),
        ('memory_usage', 62.8, strftime('%s','now')),
        ('disk_usage', 78.5, strftime('%s','now')),
        ('network_in', 1250.45, strftime('%s','now')),
        ('network_out', 876.23, strftime('%s','now'));
"@
}

# Get absolute path to SQLite database
$SQLITE_DB_ABS = (Resolve-Path $SQLITE_DB).Path

# Example queries

# 1. Get table sizes
$TABLE_SIZE_QUERY = "SELECT name, size as value FROM tables"
Write-Host "Fetching table sizes..." -ForegroundColor Green
$encodedQuery = [System.Web.HttpUtility]::UrlEncode($TABLE_SIZE_QUERY)
Invoke-RestMethod -Uri "${SERVER_URL}?type=sqlite&username=&password=&host=&db=${SQLITE_DB_ABS}&query=${encodedQuery}&metric_prefix=table_size&value_column=value"

# 2. Get table row counts
$ROW_COUNT_QUERY = "SELECT name, rows as value FROM tables"
Write-Host "`nFetching row counts..." -ForegroundColor Green
$encodedQuery = [System.Web.HttpUtility]::UrlEncode($ROW_COUNT_QUERY)
Invoke-RestMethod -Uri "${SERVER_URL}?type=sqlite&username=&password=&host=&db=${SQLITE_DB_ABS}&query=${encodedQuery}&metric_prefix=table_rows&value_column=value"

# 3. Get metrics
$METRICS_QUERY = "SELECT name, value FROM metrics"
Write-Host "`nFetching metrics..." -ForegroundColor Green
$encodedQuery = [System.Web.HttpUtility]::UrlEncode($METRICS_QUERY)
Invoke-RestMethod -Uri "${SERVER_URL}?type=sqlite&username=&password=&host=&db=${SQLITE_DB_ABS}&query=${encodedQuery}&metric_prefix=system_metrics&value_column=value"
