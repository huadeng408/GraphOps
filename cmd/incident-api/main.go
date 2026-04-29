package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"graphops/internal/incidentapi"
)

func main() {
	addr := envOrDefault("INCIDENT_API_ADDR", ":8082")
	storeType := envOrDefault("INCIDENT_STORE", "memory")

	store, cleanup := mustBuildRepository(storeType)
	defer cleanup()
	server := incidentapi.NewServer(store)

	log.Printf("incident-api listening on %s with %s store", addr, storeType)
	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func mustBuildRepository(storeType string) (incidentapi.Repository, func()) {
	switch storeType {
	case "memory":
		return incidentapi.NewMemoryStore(), func() {}
	case "mysql":
		dsn := normalizeMySQLDSN(os.Getenv("MYSQL_DSN"))
		if dsn == "" {
			log.Fatal("MYSQL_DSN is required when INCIDENT_STORE=mysql")
		}

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			log.Fatal(err)
		}
		if err := db.Ping(); err != nil {
			log.Fatal(err)
		}
		return incidentapi.NewMySQLStore(db), func() {
			_ = db.Close()
		}
	default:
		log.Fatalf("unsupported INCIDENT_STORE: %s", storeType)
		return nil, func() {}
	}
}

func normalizeMySQLDSN(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return ""
	}
	if !strings.Contains(dsn, "charset=") {
		if strings.Contains(dsn, "?") {
			dsn += "&charset=utf8mb4"
		} else {
			dsn += "?charset=utf8mb4"
		}
	}
	if !strings.Contains(dsn, "collation=") {
		if strings.Contains(dsn, "?") {
			dsn += "&collation=utf8mb4_unicode_ci"
		} else {
			dsn += "?collation=utf8mb4_unicode_ci"
		}
	}
	return dsn
}
