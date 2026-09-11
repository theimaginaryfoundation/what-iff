package database

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

const (
	EnvDBPassword = "DB_PASSWORD"
)

// getDBPassword reads the database password from the environment. In deployed
// environments ECS injects it from Secrets Manager via the task definition's
// secrets block; locally it comes from .env/docker-compose.
func getDBPassword() (string, error) {
	dbPassword := os.Getenv(EnvDBPassword)
	if dbPassword == "" {
		return "", fmt.Errorf("DB_PASSWORD environment variable is not set")
	}
	return dbPassword, nil
}

// NewClient creates a new database client connection.
// Returns both the Ent client and underlying *sql.DB for health checks (Ent doesn't expose Ping).
func NewClient(logger *zap.Logger) (*ent.Client, *sql.DB, error) {
	var dsn string
	var dbDriver string

	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbName := os.Getenv("DB_NAME")
	dbType := databaseType()

	// ECS injects DB_PASSWORD from Secrets Manager; local dev sets it in .env
	dbPass, err := getDBPassword()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get database password: %w", err)
	}

	switch dbType {
	case "postgres":
		// Fail closed: an unset DB_SSL_MODE must not silently mean no TLS.
		// Local docker-compose Postgres has no TLS listener, so it sets
		// DB_SSL_MODE=disable explicitly — that's an opt-out, not the default.
		sslMode := os.Getenv("DB_SSL_MODE")
		if sslMode == "" {
			sslMode = "require"
		}

		dbDriver = dialect.Postgres
		dsn = fmt.Sprintf("host=%s port=%s user=%s "+
			"password=%s dbname=%s sslmode=%s",
			dbHost, dbPort, dbUser, dbPass, dbName, sslMode)
	case "mysql":
		dbDriver = dialect.MySQL
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
			dbUser, dbPass, dbHost, dbPort, dbName)
	default:
		return nil, nil, fmt.Errorf("invalid database type: %q", dbType)
	}

	// Create driver with MaxIdleConns and MaxOpenConns
	drv, err := entsql.Open(dbDriver, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("failed opening connection to postgres: %w", err)
	}

	// Get the underlying sql.DB object
	db := drv.DB()
	db.SetMaxIdleConns(10)
	db.SetMaxOpenConns(100)
	db.SetConnMaxLifetime(time.Hour)

	// Create the ent client
	client := ent.NewClient(ent.Driver(drv))

	logger.Info("Database connection established", zap.String("host", dbHost), zap.String("database", dbName))
	return client, db, nil
}

// databaseType defaults to PostgreSQL because it is the only supported local and deployed
// configuration. Keeping the fallback here lets standalone local commands load the same `.env`
// used by Docker Compose, where DB_TYPE is supplied by Compose rather than the file itself.
func databaseType() string {
	if dbType := strings.TrimSpace(os.Getenv("DB_TYPE")); dbType != "" {
		return dbType
	}
	return "postgres"
}
