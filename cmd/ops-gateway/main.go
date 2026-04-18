package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"graphops/internal/opsgateway"
)

func main() {
	addr := envOrDefault("OPS_GATEWAY_ADDR", ":8085")
	storeType := envOrDefault("ACTION_RECEIPT_STORE", "memory")
	redisClient, redisCleanup := buildRedisClient()
	defer redisCleanup()

	store, cleanup := mustBuildStore(storeType, redisClient)
	defer cleanup()
	server := opsgateway.NewServer(store)

	log.Printf("ops-gateway listening on %s with %s store", addr, storeType)
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

func mustBuildStore(storeType string, redisClient *redis.Client) (opsgateway.GatewayStore, func()) {
	switch storeType {
	case "memory":
		return opsgateway.NewStore(redisClient), func() {}
	case "mysql":
		dsn := os.Getenv("MYSQL_DSN")
		if dsn == "" {
			log.Fatal("MYSQL_DSN is required when ACTION_RECEIPT_STORE=mysql")
		}
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			log.Fatal(err)
		}
		if err := db.Ping(); err != nil {
			log.Fatal(err)
		}
		return opsgateway.NewMySQLStore(db, redisClient), func() {
			_ = db.Close()
		}
	default:
		log.Fatalf("unsupported ACTION_RECEIPT_STORE: %s", storeType)
		return nil, func() {}
	}
}

func buildRedisClient() (*redis.Client, func()) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil, func() {}
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("invalid REDIS_URL: %v", err)
	}
	client := redis.NewClient(opts)
	return client, func() {
		_ = client.Close()
	}
}
