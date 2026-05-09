package main

import (
	"database/sql"
	"fmt"
	"log"
	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "./news_central.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM news").Scan(&count)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("News count: %d\n", count)
}
