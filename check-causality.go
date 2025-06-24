package main

import (
	"database/sql"
	"fmt"
	"log"
	
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "pcas.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	
	rows, err := db.Query("SELECT id, type, trace_id, correlation_id FROM events ORDER BY time")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	
	fmt.Println("=== Event Causality Chain ===")
	fmt.Println()
	
	count := 0
	for rows.Next() {
		var id, eventType string
		var traceID, correlationID sql.NullString
		
		err := rows.Scan(&id, &eventType, &traceID, &correlationID)
		if err != nil {
			log.Fatal(err)
		}
		
		count++
		fmt.Printf("Event #%d:\n", count)
		fmt.Printf("  ID: %s\n", id)
		fmt.Printf("  Type: %s\n", eventType)
		fmt.Printf("  Trace ID: %s\n", traceID.String)
		fmt.Printf("  Correlation ID: %s\n", correlationID.String)
		fmt.Println()
	}
	
	fmt.Printf("Total events: %d\n", count)
}