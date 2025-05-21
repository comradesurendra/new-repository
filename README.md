# Bulk CSV to MongoDB Processor

## Overview

This project provides a command-line tool written in Go to efficiently process CSV (Comma Separated Values) files and insert their data into a MongoDB database. It is designed to handle potentially large CSV files by reading them record by record and can be configured via command-line flags.

## Features

-   Reads CSV files record by record, making it suitable for large datasets.
-   Inserts data into a specified MongoDB database and collection.
-   Highly configurable through command-line flags for CSV file path, MongoDB URI, database name, and collection name.
-   Concurrent processing of CSV reading and MongoDB insertion (within the constraints of sequential CSV reading).
-   Structured logging for monitoring progress and errors.
-   Graceful handling of individual row processing errors, allowing the program to continue with other valid records.
-   Unit tests for core functionalities.

## Prerequisites

-   **Go:** Version 1.18 or higher installed (for `t.TempDir()` in tests; main code might work with older versions supporting modules).
-   **MongoDB:** A running MongoDB instance accessible to the machine where the tool is run.

## Build Instructions

To build the executable from the project root directory:

```bash
go build
```

This will create an executable named `bulk-csv-processor` (or `bulk-csv-processor.exe` on Windows) in the current directory.

## Configuration

The tool is configured using command-line flags:

-   `-csvFile string`
    -   Path to the CSV file to process.
    -   Default: `"input.csv"`
-   `-mongoURI string`
    -   MongoDB connection URI.
    -   Default: `"mongodb://localhost:27017"`
-   `-dbName string`
    -   MongoDB database name.
    -   Default: `"bulkcsv"`
-   `-collectionName string`
    -   MongoDB collection name where data will be inserted.
    -   Default: `"processed_data"`

## Usage Example

Assuming you have a CSV file named `data.csv` in the current directory and want to insert its contents into a MongoDB instance running on `mongodb://localhost:27017`, into the database `mydatabase` and collection `mycollection`:

```bash
./bulk-csv-processor -csvFile="data.csv" -mongoURI="mongodb://localhost:27017" -dbName="mydatabase" -collectionName="mycollection"
```

If you build the executable with a different name or are not in the project root, adjust the path to the executable accordingly.

## Error Handling & Logging

-   **Critical Errors:** Errors such as inability to connect to MongoDB or failure to open/read the CSV header will cause the program to stop execution. These are logged with an "ERROR:" prefix.
-   **Row-Level Errors:** If an error occurs while processing or inserting an individual row from the CSV (e.g., malformed CSV line, database insertion error for a single document), the error will be logged with an "ERROR:" prefix, including details of the problematic row, and the program will continue to process subsequent rows.
-   **Logging:** The program uses structured logging with "INFO:" and "ERROR:" prefixes. Timestamps are included. Logs provide information about the configuration, connection status, CSV reading progress, data insertion summaries (successful and failed counts), and any errors encountered.

## Running Tests

To run the unit tests included in the project, navigate to the project root directory and execute:

```bash
go test
```

This command will discover and run all test functions in `_test.go` files.
