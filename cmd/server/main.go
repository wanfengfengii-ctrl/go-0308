package main

import (
	"log"
	"net/http"
	"os"

	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/server"
	"example.com/potable-water-pipeline/internal/store"
	"example.com/potable-water-pipeline/internal/workflow"
)

func main() {
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "web/dist"
	}
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "potable-water-pipeline.db"
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc, err := workflow.New(st, rules.DefaultCatalog())
	if err != nil {
		log.Fatalf("init workflow: %v", err)
	}

	srv := server.New(staticDir, svc)
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	log.Printf("potable-water-pipeline listening on %s (static: %s, db: %s)", addr, staticDir, dbPath)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
