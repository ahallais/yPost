package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"usenet-poster/pkg/models"
)

// LoadConfig loads configuration from file and environment
func LoadConfig(configPath string) (*models.Config, error) {
	v := viper.New()

	// Set default values
	setDefaults(v)

	// Set config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.usenet-poster")
		v.AddConfigPath("/etc/usenet-poster")
	}

	// Read environment variables
	v.SetEnvPrefix("USENET")
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var config models.Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// NNTP defaults
	v.SetDefault("nntp.servers", []map[string]interface{}{
		{
			"host":           "news.example.com",
			"port":           119,
			"username":       "",
			"password":       "",
			"ssl":            false,
			"max_connections": 4,
		},
	})

	// Posting defaults
	v.SetDefault("posting.group", "alt.binaries.test")
	v.SetDefault("posting.poster_name", "Usenet Poster")
	v.SetDefault("posting.poster_email", "poster@example.com")
	v.SetDefault("posting.subject_template", "{{.FileName}} [{{.PartNumber}}/{{.TotalParts}}]")
	v.SetDefault("posting.max_line_length", 128)
	v.SetDefault("posting.max_part_size", 750*1024) // 750KB
	v.SetDefault("posting.custom_headers", map[string]string{})

	// Output defaults
	v.SetDefault("output.output_dir", "./output")
	v.SetDefault("output.nzb_dir", "./output/nzb")
	v.SetDefault("output.log_dir", "./output/logs")

	// Features defaults
	v.SetDefault("features.create_par2", true)
	v.SetDefault("features.create_sfv", true)
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
	v.Set("features", config.Features)

	return v.WriteConfigAs(configPath)
}

// CreateSampleConfig creates a sample configuration file
func CreateSampleConfig(configPath string) error {
	sampleConfig := &models.Config{}

	// Set sample values
	sampleConfig.NNTP.Servers = []models.ServerConfig{
		{
			Host:     "news.example.com",
			Port:     119,
			Username: "your_username",
			Password: "your_password",
			SSL:      false,
			MaxConns: 4,
		},
		{
			Host:     "ssl.news.example.com",
			Port:     563,
			Username: "your_username",
			Password: "your_password",
			SSL:      true,
			MaxConns: 8,
		},
	}

	sampleConfig.Posting.Group = "alt.binaries.test"
	sampleConfig.Posting.PosterName = "Your Name"
	sampleConfig.Posting.PosterEmail = "your.email@example.com"
	sampleConfig.Posting.SubjectTemplate = "{{.FileName}} [{{.PartNumber}}/{{.TotalParts}}] - {{.FileSize}}"
	sampleConfig.Posting.MaxLineLength = 128
	sampleConfig.Posting.MaxPartSize = 750 * 1024
	sampleConfig.Posting.CustomHeaders = map[string]string{
		"X-Usenet-Tool": "usenet-poster",
	}

	sampleConfig.Output.OutputDir = "./output"
	sampleConfig.Output.NZBDir = "./output/nzb"
	sampleConfig.Output.LogDir = "./output/logs"

	sampleConfig.Features.CreatePAR2 = true
	sampleConfig.Features.CreateSFV = true

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
		homeConfig := filepath.Join(homeDir, ".usenet-poster", "config.yaml")
		if _, err := os.Stat(homeConfig); err == nil {
			return homeConfig
		}
	}

	// Return default
	return "config.yaml"
}