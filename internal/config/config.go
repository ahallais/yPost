package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"ypost/pkg/models"
)

// LoadConfig loads configuration from file and environment
func LoadConfig(configPath string) (*models.Config, string, error) {
	v := viper.New()

	// Set default values
	setDefaults(v)

	// Set config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		// Search paths in order:
		v.AddConfigPath(".")                // 1. Current directory
		v.AddConfigPath("$HOME/.ypost")     // 2. User's home directory
		v.AddConfigPath("/etc/ypost")       // 3. System-wide configuration
	}

	// Read environment variables
	v.SetEnvPrefix("USENET")
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, "", fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var config models.Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Handle legacy configuration format (backward compatibility)
	if len(config.NNTP.Servers) == 0 {
		// Check if legacy format is used
		if config.NNTP.Server != "" {
			// Convert legacy format to new format
			server := models.ServerConfig{
				Host:     config.NNTP.Server,
				Port:     config.NNTP.Port,
				Username: config.NNTP.Username,
				Password: config.NNTP.Password,
				SSL:      config.NNTP.SSL,
				MaxConns: config.NNTP.Connections,
			}
			if server.Port == 0 {
				server.Port = 563 // Default NNTP SSL port
			}
			if server.MaxConns == 0 {
				server.MaxConns = 4 // Default connections
			}
			config.NNTP.Servers = []models.ServerConfig{server}
		}
	}

	// Validate configuration
	if err := validateConfig(&config); err != nil {
		return nil, "", fmt.Errorf("invalid configuration: %w", err)
	}

	configFileUsed := v.ConfigFileUsed()
	return &config, configFileUsed, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// NNTP defaults - don't set servers default to allow legacy format
	v.SetDefault("nntp.server", "your-newsserver.com")
	v.SetDefault("nntp.port", 563)
	v.SetDefault("nntp.username", "your-username")
	v.SetDefault("nntp.password", "your-password")
	v.SetDefault("nntp.ssl", true)
	v.SetDefault("nntp.connections", 4)

	// Posting defaults
	v.SetDefault("posting.group", "alt.binaries.test")
	v.SetDefault("posting.poster_email", "poster@example.com")
	v.SetDefault("posting.subject_template", "[{{.Index}}/{{.Total}}] - {{.Filename}} - ({{.Size}})")
	v.SetDefault("posting.max_line_length", 128)
	v.SetDefault("posting.max_part_size", 750000)

	// Output defaults
	v.SetDefault("output.output_dir", "output")
	v.SetDefault("output.nzb_dir", "output/nzb")
	v.SetDefault("output.log_dir", "output/logs")

	// Splitting defaults
	v.SetDefault("splitting.max_file_size", "50MB")
	v.SetDefault("splitting.max_lines", 5000)

	// Par2 defaults
	v.SetDefault("par2.redundancy", 10)
	v.SetDefault("par2.enabled", true)

	// SFV defaults
	v.SetDefault("sfv.enabled", true)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "ypost.log")
}

// validateConfig validates the configuration
func validateConfig(config *models.Config) error {
	if len(config.NNTP.Servers) == 0 {
		return fmt.Errorf("at least one NNTP server must be configured")
	}

	for i, server := range config.NNTP.Servers {
		if server.Host == "" {
			return fmt.Errorf("server %d: host is required", i+1)
		}
		if server.Port <= 0 || server.Port > 65535 {
			return fmt.Errorf("server %d: invalid port %d", i+1, server.Port)
		}
		if server.MaxConns <= 0 || server.MaxConns > 50 {
			server.MaxConns = 4 // Default
		}
	}

	if config.Posting.Group == "" {
		return fmt.Errorf("posting group is required")
	}

	if config.Posting.MaxPartSize <= 0 {
		return fmt.Errorf("max part size must be positive")
	}

	if config.Posting.MaxLineLength <= 0 {
		return fmt.Errorf("max line length must be positive")
	}

	return nil
}

// SaveConfig saves configuration to file
func SaveConfig(config *models.Config, configPath string) error {
	if configPath == "" {
		configPath = "config.yaml"
	}

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	// Use viper to marshal and save
	v := viper.New()
	v.Set("nntp", config.NNTP)
	v.Set("posting", config.Posting)
	v.Set("output", config.Output)
	v.Set("splitting", config.Splitting)
	v.Set("par2", config.Par2)
	v.Set("sfv", config.SFV)
	v.Set("logging", config.Logging)

	return v.WriteConfigAs(configPath)
}

// CreateSampleConfig creates a sample configuration file
func CreateSampleConfig(configPath string) error {
	sampleConfig := &models.Config{}

	// NNTP configuration
	defaultServer := models.ServerConfig{
		Host:     "your-newsserver.com",
		Port:     563,
		Username: "your-username",
		Password: "your-password",
		SSL:      true,
		MaxConns: 8,
	}
	sampleConfig.NNTP.Servers = []models.ServerConfig{defaultServer}

	// Posting configuration
	sampleConfig.Posting.Group = "alt.binaries.test"
	sampleConfig.Posting.PosterEmail = "poster@example.com"
	sampleConfig.Posting.SubjectTemplate = "[{{.Index}}/{{.Total}}] - {{.Filename}} - ({{.Size}})"
	sampleConfig.Posting.MaxLineLength = 128
	sampleConfig.Posting.MaxPartSize = 750000

	// Output configuration
	sampleConfig.Output.OutputDir = "output"
	sampleConfig.Output.NZBDir = "output/nzb"
	sampleConfig.Output.LogDir = "output/logs"

	// Splitting configuration
	sampleConfig.Splitting.MaxFileSize = "50MB"
	sampleConfig.Splitting.MaxLines = 5000

	// Par2 configuration
	sampleConfig.Par2.Redundancy = 10
	sampleConfig.Par2.Enabled = true

	// SFV configuration
	sampleConfig.SFV.Enabled = true

	// Logging configuration
	sampleConfig.Logging.Level = "info"
	sampleConfig.Logging.File = "ypost.log"

	return SaveConfig(sampleConfig, configPath)
}

// GetConfigPath returns the default config path
func GetConfigPath() string {
	// Check for config in current directory
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}

	// Check for config in home directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		homeConfig := filepath.Join(homeDir, ".ypost", "config.yaml")
		if _, err := os.Stat(homeConfig); err == nil {
			return homeConfig
		}
	}

	// Return default
	return "config.yaml"
}