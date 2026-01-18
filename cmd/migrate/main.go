package main

import (
	"fmt"
	"log"
	"os"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: migrate <up|down|version>")
		os.Exit(1)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://postgres:postgres@localhost:5432/rescuestream?sslmode=disable"
	}

	command := os.Args[1]

	switch command {
	case "up":
		if err := database.RunMigrations(databaseURL); err != nil {
			log.Fatalf("Failed to run migrations: %v", err)
		}
		fmt.Println("Migrations applied successfully")

	case "down":
		if err := database.RollbackMigrations(databaseURL); err != nil {
			log.Fatalf("Failed to rollback migration: %v", err)
		}
		fmt.Println("Migration rolled back successfully")

	case "version":
		version, dirty, err := database.MigrationVersion(databaseURL)
		if err != nil {
			log.Fatalf("Failed to get migration version: %v", err)
		}
		if dirty {
			fmt.Printf("Version: %d (dirty)\n", version)
		} else {
			fmt.Printf("Version: %d\n", version)
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		fmt.Println("Usage: migrate <up|down|version>")
		os.Exit(1)
	}
}
