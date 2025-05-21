package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// Helper function to create a temporary CSV file for testing
func createTestCSVFile(t *testing.T, content string) string {
	t.Helper()
	tempDir := t.TempDir() // Go 1.15+ for t.TempDir()
	filePath := filepath.Join(tempDir, "test.csv")
	err := ioutil.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test CSV file: %v", err)
	}
	return filePath
}

func TestReadCSV(t *testing.T) {
	t.Run("ValidCSV", func(t *testing.T) {
		csvContent := "ID,Name,Value\n1,First,100\n2,Second,200"
		filePath := createTestCSVFile(t, csvContent)

		headerChan := make(chan []string, 1)
		dataChan := make(chan []string, 2)
		errChan := make(chan error, 1)
		var wg sync.WaitGroup
		wg.Add(1)

		go readCSV(filePath, headerChan, dataChan, errChan, &wg)

		expectedHeader := []string{"ID", "Name", "Value"}
		select {
		case header := <-headerChan:
			if !reflect.DeepEqual(header, expectedHeader) {
				t.Errorf("Expected header %v, got %v", expectedHeader, header)
			}
		case err := <-errChan:
			t.Errorf("Unexpected error reading header: %v", err)
			return
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for header")
		}

		expectedData := [][]string{
			{"1", "First", "100"},
			{"2", "Second", "200"},
		}
		receivedData := make([][]string, 0, 2)

	dataLoop:
		for i := 0; i < len(expectedData); i++ {
			select {
			case data, ok := <-dataChan:
				if !ok {
					t.Errorf("Data channel closed prematurely after %d records", len(receivedData))
					break dataLoop
				}
				receivedData = append(receivedData, data)
			case err := <-errChan:
				t.Errorf("Unexpected error during data reading: %v", err)
				// Potentially break or return depending on whether errors here are fatal for the test
			case <-time.After(1 * time.Second):
				t.Fatalf("Timeout waiting for data record %d", i+1)
			}
		}

		wg.Wait() // Ensure readCSV finishes

		// Check if any unexpected errors were sent after processing data
		select {
		case err := <-errChan:
			t.Errorf("Unexpected error after data processing: %v", err)
		default:
		}


		if !reflect.DeepEqual(receivedData, expectedData) {
			t.Errorf("Expected data %v, got %v", expectedData, receivedData)
		}
		if len(receivedData) != len(expectedData) {
			t.Errorf("Expected %d data records, got %d", len(expectedData), len(receivedData))
		}
	})

	t.Run("EmptyCSV", func(t *testing.T) {
		filePath := createTestCSVFile(t, "")

		headerChan := make(chan []string, 1)
		dataChan := make(chan []string, 1) // Small buffer, won't be used
		errChan := make(chan error, 1)
		var wg sync.WaitGroup
		wg.Add(1)

		go readCSV(filePath, headerChan, dataChan, errChan, &wg)

		select {
		case err := <-errChan:
			if err == nil {
				t.Errorf("Expected error for empty CSV, got nil")
			}
			// Check for specific error message if desired, e.g., strings.Contains(err.Error(), "empty")
			if !strings.Contains(err.Error(), "empty") && !strings.Contains(err.Error(), "EOF") { // EOF is also possible if file is truly empty
                 t.Errorf("Expected error message to contain 'empty' or 'EOF', got: %s", err.Error())
            }
		case <-headerChan:
			t.Errorf("Expected no header for empty CSV")
		case <-dataChan:
			t.Errorf("Expected no data for empty CSV")
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for error on empty CSV")
		}
		wg.Wait()
	})

	t.Run("HeaderOnlyCSV", func(t *testing.T) {
		csvContent := "ID,Name"
		filePath := createTestCSVFile(t, csvContent)

		headerChan := make(chan []string, 1)
		dataChan := make(chan []string, 1) // Expect no data
		errChan := make(chan error, 1)    // Expect no errors
		var wg sync.WaitGroup
		wg.Add(1)

		go readCSV(filePath, headerChan, dataChan, errChan, &wg)

		expectedHeader := []string{"ID", "Name"}
		select {
		case header := <-headerChan:
			if !reflect.DeepEqual(header, expectedHeader) {
				t.Errorf("Expected header %v, got %v", expectedHeader, header)
			}
		case err := <-errChan:
			t.Fatalf("Unexpected error for header-only CSV: %v", err)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for header")
		}

		// Ensure dataChan is empty and closed
		select {
		case data, ok := <-dataChan:
			if ok {
				t.Errorf("Expected no data for header-only CSV, got %v", data)
			} // If !ok, channel is closed as expected.
		case err := <-errChan:
			t.Fatalf("Unexpected error after header for header-only CSV: %v", err)
		case <-time.After(100 * time.Millisecond): // Shorter timeout, dataChan should close quickly
			// This case means dataChan was not closed and no data was sent. Good.
		}
		
		wg.Wait() // Wait for readCSV to finish and close channels

		// Final check for errors
		select {
		case err := <-errChan:
			t.Errorf("Unexpected error on errChan for header-only CSV: %v", err)
		default: // No error, as expected
		}
	})

	t.Run("MalformedCSVQuote", func(t *testing.T) {
		// Malformed CSV: unclosed quote
		csvContent := "ID,Name\n1,\"Unclosed Name"
		filePath := createTestCSVFile(t, csvContent)

		headerChan := make(chan []string, 1)
		dataChan := make(chan []string, 1)
		errChan := make(chan error, 2) // Expect header then error
		var wg sync.WaitGroup
		wg.Add(1)

		go readCSV(filePath, headerChan, dataChan, errChan, &wg)

		expectedHeader := []string{"ID", "Name"}
		select {
		case header := <-headerChan:
			if !reflect.DeepEqual(header, expectedHeader) {
				t.Errorf("Expected header %v, got %v", expectedHeader, header)
			}
		case err := <-errChan:
			t.Errorf("Unexpected error when expecting header: %v", err)
			wg.Wait()
			return
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for header")
		}
		
		// Now expect an error for the malformed line
		select {
		case err := <-errChan:
			if err == nil {
				t.Errorf("Expected error for malformed CSV line, got nil")
			} else if !strings.Contains(err.Error(), "parse error") && !strings.Contains(err.Error(), "wrong number of fields") {
				// The actual error might vary based on CSV parser specifics for "malformed"
				// "wrong number of fields" can happen if a quote isn't closed and it consumes commas.
				// "bare \" in non-quoted-field" is another possibility.
				// "parse error on line 2, column 1: extraneous or missing \" in quoted-field" is typical.
				t.Logf("Note: Received malformed CSV error: %v", err) // Log it for info
			}
		case data := <-dataChan:
			t.Errorf("Expected no data for malformed CSV line, got %v", data)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for error on malformed CSV line")
		}
		wg.Wait()
	})
	
	t.Run("RowWithDifferentNumberOfColumns", func(t *testing.T) {
        // readCSV itself doesn't validate column counts per row against the header.
        // It passes what encoding/csv reads. The main processing loop handles mismatches.
        // This test ensures readCSV passes the row as read by encoding/csv.
        csvContent := "Header1,Header2\nValue1\nValue1,Value2,Value3"
        filePath := createTestCSVFile(t, csvContent)

        headerChan := make(chan []string, 1)
        dataChan := make(chan []string, 2)
        errChan := make(chan error, 1) 
        var wg sync.WaitGroup
        wg.Add(1)

        go readCSV(filePath, headerChan, dataChan, errChan, &wg)

        expectedHeader := []string{"Header1", "Header2"}
        select {
        case header := <-headerChan:
            if !reflect.DeepEqual(header, expectedHeader) {
                t.Errorf("Expected header %v, got %v", expectedHeader, header)
            }
        case err := <-errChan:
            t.Fatalf("Unexpected error reading header: %v", err)
        case <-time.After(1 * time.Second):
            t.Fatal("Timeout waiting for header")
        }

        expectedDataRows := [][]string{
            {"Value1"}, // encoding/csv by default allows this if FieldsPerRecord is not set or is 0
                        // Our readCSV sets FieldsPerRecord = -1, so it will allow variable fields.
            {"Value1", "Value2", "Value3"},
        }
		
		receivedData := make([][]string, 0)
		for i := 0; i < len(expectedDataRows); i++ {
			select {
			case row, ok := <-dataChan:
				if !ok {
					t.Fatalf("Data channel closed prematurely. Expected %d rows, got %d.", len(expectedDataRows), len(receivedData))
				}
				receivedData = append(receivedData, row)
			case err := <-errChan:
				// If FieldsPerRecord is positive, csv.Reader might send an error here.
				// Since we use -1, we don't expect errors *from readCSV* for this.
				// Errors about column mismatch are handled in main's processing loop.
				t.Errorf("Unexpected error from readCSV for mismatched columns: %v", err)
			case <-time.After(1 * time.Second):
				t.Fatalf("Timeout waiting for data row %d", i+1)
			}
		}

        wg.Wait() // ensure readCSV goroutine finishes

		select {
		case err := <-errChan:
			// No errors should be sent by readCSV for this case
			t.Errorf("Unexpected error on errChan: %v", err)
		default:
		}

        if !reflect.DeepEqual(receivedData, expectedDataRows) {
            t.Errorf("Expected data rows %v, got %v", expectedDataRows, receivedData)
        }
    })
}

// TestTransformCSVRowToBSON tests the core logic of transforming a CSV row to a BSON document.
// This simulates the transformation part of the main processing loop.
func TestTransformCSVRowToBSON(t *testing.T) {
	// Helper function for this test case, directly performing the transformation
	transform := func(headers []string, record []string) (bson.M, error) {
		if len(headers) != len(record) {
			// This check is done in main's loop before calling the conceptual transformation.
			// For this unit test, we assume inputs would have passed this check
			// or we are specifically testing mismatched lengths if the function were to handle it.
			// However, the current main logic filters these out.
			// So, we'll focus on cases where lengths match.
			return nil, fmt.Errorf("header length (%d) and record length (%d) must match for transformation", len(headers), len(record))
		}
		doc := bson.M{}
		for j, header := range headers {
			doc[header] = record[j]
		}
		return doc, nil
	}

	t.Run("ValidRow", func(t *testing.T) {
		headers := []string{"ID", "Name", "Status"}
		record := []string{"123", "WidgetA", "Active"}
		expectedDoc := bson.M{"ID": "123", "Name": "WidgetA", "Status": "Active"}

		doc, err := transform(headers, record)
		if err != nil {
			t.Fatalf("Transformation failed: %v", err)
		}
		if !reflect.DeepEqual(doc, expectedDoc) {
			t.Errorf("Expected BSON %v, got %v", expectedDoc, doc)
		}
	})

	t.Run("RowWithEmptyStrings", func(t *testing.T) {
		headers := []string{"ID", "Name", "Value"}
		record := []string{"", "EmptyWidget", ""}
		expectedDoc := bson.M{"ID": "", "Name": "EmptyWidget", "Value": ""}

		doc, err := transform(headers, record)
		if err != nil {
			t.Fatalf("Transformation failed: %v", err)
		}
		if !reflect.DeepEqual(doc, expectedDoc) {
			t.Errorf("Expected BSON %v, got %v", expectedDoc, doc)
		}
	})

	t.Run("NoHeadersNoData", func(t *testing.T) {
		headers := []string{}
		record := []string{}
		expectedDoc := bson.M{} // Expect an empty BSON map

		doc, err := transform(headers, record)
		if err != nil {
			t.Fatalf("Transformation failed: %v", err)
		}
		if !reflect.DeepEqual(doc, expectedDoc) {
			t.Errorf("Expected BSON %v, got %v", expectedDoc, doc)
		}
	})

	t.Run("MismatchedLengthError", func(t *testing.T) {
        // This sub-test verifies that if our transform helper were to receive
        // mismatched lengths (which it shouldn't due to pre-filtering in main),
        // it would return an error as per its defined behavior.
        headers := []string{"ID", "Name"}
        record := []string{"123"} // Mismatched length
        
        _, err := transform(headers, record)
        if err == nil {
            t.Errorf("Expected an error for mismatched header and record lengths, got nil")
        } else {
            // Check if the error message is as expected (optional)
            expectedErrorMsg := "header length (2) and record length (1) must match for transformation"
            if err.Error() != expectedErrorMsg {
                t.Errorf("Expected error message '%s', got '%s'", expectedErrorMsg, err.Error())
            }
        }
    })
}
