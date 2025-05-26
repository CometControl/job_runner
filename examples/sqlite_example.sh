#!/bin/bash
# Example script to query SQLite and get metrics

# Set SQLite database path - using the test database for this example
SQLITE_DB="./tests/test_data.db"

# Set the Job Runner URL
SERVER_URL="http://localhost:8080/metrics"

# Check if test database exists, create it if it doesn't
if [ ! -f "$SQLITE_DB" ]; then
    echo -e "\033[33mCreating test SQLite database...\033[0m"
    
    # Create directory if it doesn't exist
    mkdir -p $(dirname "$SQLITE_DB")
    
    # Create tables
    sqlite3 "$SQLITE_DB" <<EOF
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
EOF
fi

# Get absolute path to SQLite database
SQLITE_DB_ABS=$(realpath "$SQLITE_DB")

# Example queries

# 1. Get table sizes
TABLE_SIZE_QUERY="SELECT name, size as value FROM tables"
echo -e "\033[32mFetching table sizes...\033[0m"
ENCODED_QUERY=$(python -c "import urllib.parse; print(urllib.parse.quote('''$TABLE_SIZE_QUERY'''))")
curl -s "${SERVER_URL}?type=sqlite&username=&password=&host=&db=${SQLITE_DB_ABS}&query=${ENCODED_QUERY}&metric_prefix=table_size&value_column=value"

# 2. Get table row counts
ROW_COUNT_QUERY="SELECT name, rows as value FROM tables"
echo -e "\n\033[32mFetching row counts...\033[0m"
ENCODED_QUERY=$(python -c "import urllib.parse; print(urllib.parse.quote('''$ROW_COUNT_QUERY'''))")
curl -s "${SERVER_URL}?type=sqlite&username=&password=&host=&db=${SQLITE_DB_ABS}&query=${ENCODED_QUERY}&metric_prefix=table_rows&value_column=value"

# 3. Get metrics
METRICS_QUERY="SELECT name, value FROM metrics"
echo -e "\n\033[32mFetching metrics...\033[0m"
ENCODED_QUERY=$(python -c "import urllib.parse; print(urllib.parse.quote('''$METRICS_QUERY'''))")
curl -s "${SERVER_URL}?type=sqlite&username=&password=&host=&db=${SQLITE_DB_ABS}&query=${ENCODED_QUERY}&metric_prefix=system_metrics&value_column=value"
