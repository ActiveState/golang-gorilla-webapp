package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"

	_ "github.com/lib/pq"
)

// This isn't very elegant but it gets the job done. If this were a real app
// we'd use something like Sqitch (http://sqitch.org/) to manage the schema,
// but for the purposes of our demo app we only want to require ActiveGo.
func main() {
	var dbName string
	flag.StringVar(&dbName, "db", "hashtext", "the name of the database to create")
	flag.Parse()

	fmt.Printf("(Re-)Building the %s database\n", dbName)
	fmt.Println("  This script connects as a user named 'hashtext' with the password 'hashtext'")
	fmt.Println("  to the host 127.0.0.1")
	fmt.Print("\n")

	createDB(dbName)
	runDDL(dbName)

	fmt.Print("\n")
	fmt.Println("The hashtext database has been (re-)created")
	os.Exit(0)
}

func createDB(dbName string) {
	db := connectToDB("template1")

	execWithCheck(db, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	execWithCheck(db, fmt.Sprintf("CREATE DATABASE %s ENCODING=UTF8", dbName))

	err := db.Close()
	if err != nil {
		fmt.Println("** Error closing database: " + err.Error())
		os.Exit(1)
	}
}

func runDDL(dbName string) {
	db := connectToDB(dbName)

	ddl, err := ioutil.ReadFile("../schema.sql")
	if err != nil {
		fmt.Println("** Could not read the ../schema.sql file")
		os.Exit(1)
	}

	for _, s := range regexp.MustCompile("(?s:(.+?));\\n*").FindAllStringSubmatch(string(ddl), -1) {
		execWithCheck(db, s[1])
	}
}

func connectToDB(name string) *sql.DB {
	db, err := sql.Open("postgres", fmt.Sprintf("user=hashtext password=hashtext dbname=%s host=127.0.0.1", name))
	if err != nil {
		fmt.Println("** Error connecting to the " + name + " database as user hashtext: " + err.Error())
		os.Exit(1)
	}

	return db
}

func execWithCheck(db *sql.DB, s string, args ...interface{}) {
	fmt.Println(s)
	fmt.Println("----")
	_, err := db.Exec(s, args...)
	if err != nil {
		fmt.Println("** Error executing SQL - " + err.Error() + ": " + s)
		os.Exit(1)
	}
}
