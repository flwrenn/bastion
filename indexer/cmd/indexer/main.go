package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"bastion-indexer","status":"ok"}`)
	})

	log.Printf("Indexer API listening on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
