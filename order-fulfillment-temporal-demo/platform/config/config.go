package config

// Config manages application configuration using Viper
// Responsibilities:
// - Load configuration from files and environment
// - Provide typed configuration access
// - Support multiple environments

import (
	"github.com/spf13/viper"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig
	Temporal TemporalConfig
	Database DatabaseConfig
	Logger   LoggerConfig
}

// ServerConfig contains HTTP server configuration
type ServerConfig struct {
	Port            string
	Host            string
	ReadTimeout     int
	WriteTimeout    int
	ShutdownTimeout int
}

// TemporalConfig contains Temporal connection configuration
type TemporalConfig struct {
	HostPort  string
	Namespace string
	TaskQueue string
}

// DatabaseConfig contains database connection configuration
type DatabaseConfig struct {
	Driver          string
	Host            string
	Port            int
	Database        string
	Username        string
	Password        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime int
}

// LoggerConfig contains logger configuration
type LoggerConfig struct {
	Level       string
	Environment string
	OutputPaths []string
}

// Load loads configuration from file and environment
func Load(configPath string) (*Config, error) {
	// TODO: Set config file path
	// TODO: Read config file
	// TODO: Read environment variables
	// TODO: Unmarshal into Config struct
	return nil, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// TODO: Validate required fields
	return nil
}

func init() {
	viper.SetDefault("server.port", "8080")
	viper.SetDefault("temporal.namespace", "default")
	viper.SetDefault("temporal.taskqueue", "order-fulfillment")
	viper.SetDefault("logger.level", "info")
}
