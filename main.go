package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io" // Added for io.EOF
	"log"
	"os"
	"sync" // Added for WaitGroup
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	logInfoPrefix  = "INFO: "
	logErrorPrefix = "ERROR: "
)

// connectToDB establishes a connection to a MongoDB server.
func connectToDB(uri string) (*mongo.Client, error) {
	log.Println(logInfoPrefix, "Connecting to MongoDB at", uri)
	clientOptions := options.Client().ApplyURI(uri)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("could not connect to MongoDB: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		if client != nil {
			_ = client.Disconnect(context.TODO())
		}
		return nil, fmt.Errorf("could not ping MongoDB: %w", err)
	}
	log.Println(logInfoPrefix, "Successfully connected to MongoDB!")
	return client, nil
}

// insertData inserts data into a specified collection.
func insertData(client *mongo.Client, dbName string, collectionName string, data interface{}) (*mongo.InsertOneResult, error) {
	collection := client.Database(dbName).Collection(collectionName)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := collection.InsertOne(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("could not insert data into %s.%s: %w", dbName, collectionName, err)
	}
	return result, nil
}

// readCSV opens and reads a CSV file record by record, sending header and data over channels.
func readCSV(filePath string, headerChan chan<- []string, dataChan chan<- []string, errChan chan<- error, wg *sync.WaitGroup) {
	defer wg.Done() // Signal that this goroutine has finished
	defer close(headerChan)
	defer close(dataChan)
	// Not closing errChan from here as main might still be listening or other goroutines could use it.
	// However, for this specific setup, main stops on first readCSV error.

	log.Printf("%sOpening CSV file: %s", logInfoPrefix, filePath)
	file, err := os.Open(filePath)
	if err != nil {
		errChan <- fmt.Errorf("%serror opening file %s: %w", logErrorPrefix, filePath, err)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1 // Allow variable number of fields per record

	// Read header
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			errChan <- fmt.Errorf("%sCSV file %s is empty or contains only a header", logErrorPrefix, filePath)
		} else {
			errChan <- fmt.Errorf("%serror reading header from CSV %s: %w", logErrorPrefix, filePath, err)
		}
		return
	}
	headerChan <- header

	// Read records
	lineNumber := 1 // Header was line 1
	for {
		lineNumber++
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				log.Printf("%sFinished reading CSV file %s", logInfoPrefix, filePath)
				break // End of file
			}
			// Report error for this specific line and continue
			errChan <- fmt.Errorf("%serror reading record at line %d from CSV %s: %w. Skipping row", logErrorPrefix, lineNumber, filePath, err)
			continue
		}
		dataChan <- record
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix) // Use standard flags + allow prefix
	log.Println(logInfoPrefix, "Program starting...")

	// Define command-line flags
	csvFilePtr := flag.String("csvFile", "input.csv", "Path to the CSV file to process.")
	mongoURIPtr := flag.String("mongoURI", "mongodb://localhost:27017", "MongoDB connection URI.")
	dbNamePtr := flag.String("dbName", "bulkcsv", "MongoDB database name.")
	collectionNamePtr := flag.String("collectionName", "processed_data", "MongoDB collection name.")

	flag.Parse()

	mongoURI := *mongoURIPtr
	dbName := *dbNamePtr
	collectionName := *collectionNamePtr
	csvFilePath := *csvFilePtr

	log.Printf("%sConfiguration: CSVFile='%s', MongoURI='%s', DBName='%s', CollectionName='%s'",
		logInfoPrefix, csvFilePath, mongoURI, dbName, collectionName)

	client, err := connectToDB(mongoURI)
	if err != nil {
		log.Fatalf("%sMongoDB connection error: %s", logErrorPrefix, err)
	}
	defer func() {
		log.Println(logInfoPrefix, "Attempting to disconnect from MongoDB...")
		if discErr := client.Disconnect(context.TODO()); discErr != nil {
			log.Printf("%sError disconnecting from MongoDB: %s", logErrorPrefix, discErr)
		} else {
			log.Println(logInfoPrefix, "Disconnected from MongoDB successfully.")
		}
	}()

	headerChan := make(chan []string)
	dataChan := make(chan []string)
	errChan := make(chan error, 10) // Buffered error channel

	var wg sync.WaitGroup
	wg.Add(1) // For the readCSV goroutine
	go readCSV(csvFilePath, headerChan, dataChan, errChan, &wg)

	var headers []string
	var recordsProcessed, successfulInserts, failedInserts int

	// Phase 1: Receive header or critical error from readCSV
	select {
	case h, ok := <-headerChan:
		if !ok {
			log.Fatalf("%sFailed to receive header: header channel closed unexpectedly.", logErrorPrefix)
		}
		headers = h
		log.Printf("%sReceived CSV headers: %v", logInfoPrefix, headers)
	case err := <-errChan:
		log.Fatalf("%sCritical error during CSV reading setup: %s", logErrorPrefix, err)
	case <-time.After(10 * time.Second): // Timeout for header reading
		log.Fatalf("%sTimeout waiting for CSV header.", logErrorPrefix)
	}

	// Phase 2: Process data records and non-critical errors
	log.Printf("%sStarting data insertion into MongoDB: %s.%s", logInfoPrefix, dbName, collectionName)
	running := true
	for running {
		select {
		case record, ok := <-dataChan:
			if !ok { // dataChan closed by readCSV, means reading is done
				running = false // Exit loop after this select block finishes
				break
			}
			recordsProcessed++
			if len(record) != len(headers) {
				log.Printf("%sSkipping record %d (line approx %d): number of fields (%d) does not match header count (%d). Record: %v", logErrorPrefix, recordsProcessed, recordsProcessed+1, len(record), len(headers), record)
				failedInserts++
				continue
			}

			doc := bson.M{}
			for j, header := range headers {
				doc[header] = record[j]
			}

			_, insertErr := insertData(client, dbName, collectionName, doc)
			if insertErr != nil {
				log.Printf("%sError inserting record %d (line approx %d, data %v) into MongoDB: %s", logErrorPrefix, recordsProcessed, recordsProcessed+1, doc, insertErr)
				failedInserts++
			} else {
				successfulInserts++
			}
		case err := <-errChan: // Non-critical errors from readCSV (e.g., a single bad row)
			log.Printf("%sNon-critical error during CSV processing: %s", logErrorPrefix, err)
			// Depending on the error type, you might increment failedInserts or handle differently
		case <-time.After(30 * time.Second): // Overall timeout if no activity
		    if recordsProcessed == 0 && successfulInserts == 0 && failedInserts == 0 {
				// Only timeout if absolutely nothing is happening.
				// If data is flowing, this timeout won't (and shouldn't) trigger.
				log.Printf("%sTimeout waiting for data or completion. Assuming CSV processing is stalled or finished.", logErrorPrefix)
				running = false
			} else {
				// If we are processing, reset a conceptual activity timer
				// This simple timeout isn't perfect for long-running jobs, but good for now.
				log.Println(logInfoPrefix, "Activity detected, extending processing window.")
			}

		}
	}

	wg.Wait() // Wait for readCSV goroutine to fully complete (e.g. close files)
	close(errChan) // Close errChan now that producer (readCSV) and consumer loops are done

	// Drain any remaining errors from errChan, just in case
	for err := range errChan {
		log.Printf("%sPost-loop error from CSV processing: %s", logErrorPrefix, err)
	}


	log.Printf("%sCSV processing finished. Records processed: %d", logInfoPrefix, recordsProcessed)
	log.Printf("%sData insertion summary: %d successful, %d failed.", logInfoPrefix, successfulInserts, failedInserts)
	log.Println(logInfoPrefix, "Program finished.")
}
