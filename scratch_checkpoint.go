package main

import (
	"database/sql"
	"log"
	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "./news_central.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec("PRAGMA wal_checkpoint(FULL);")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Checkpoint completed successfully.")
}
