package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
)

var db *sql.DB

func main() {
	db = openDB()
	defer db.Close()

	r := makeRouter()
	http.Handle("/", r)
}

func openDB() *sql.DB {
	dbName := os.Getenv("HASHTEXT_DB")
	if dbName == "" {
		dbName = "hashtext"
	}
	db, err := sql.Open("postgres", fmt.Sprintf("user=hashtext password=hashtext dbname=%s host=127.0.0.1", dbName))
	if err != nil {
		log.Fatalf("Error connecting to the %s database as user hashtext: %v", dbName, err)
	}

	return db
}
