# Usenet Poster


Disclaimer: This was done with Vibe Coding. I'm not familiar with Go, so I relied entirely on vibe coding here. Any feedback or improvements are welcome!


A powerful, feature-rich command-line tool for posting files to Usenet newsgroups with comprehensive encoding, splitting, and recovery features.

## ✨ Features

- **yEnc Encoding** - Full yEnc encoding/decoding with streaming support for efficient file processing
- **File Splitting** - Configurable file splitting based on size/line limits to meet newsgroup requirements
- **NZB Generation** - Standard NZB file creation in XML format for easy downloading
- **PAR2 Recovery Files** - Required PAR2 generation with configurable redundancy levels for data recovery
- **SFV Checksum Files** - Required SFV creation for file integrity verification
- **NNTP Client** - Complete NNTP client with connection pooling and SSL support
- **CLI Interface** - Cobra-based CLI with intuitive commands for posting and configuration
- **Configuration Management** - YAML-based configuration with environment variable support
- **Logging System** - Comprehensive logging with history tracking for debugging and monitoring
- **Documentation** - Complete README with usage examples and configuration guide

## 🏗️ Project Structure

```
yPost/
├── cmd/                    # CLI commands (root, post, config)
├── internal/              # Internal packages
│   ├── config/            # Configuration management
│   ├── logger/            # Logging system
│   ├── nntp/              # NNTP client with connection pooling
│   ├── nzb/               # NZB file generation
│   ├── par2/              # PAR2 recovery file generation
│   ├── sfv/               # SFV checksum file generation
│   ├── splitter/          # File splitting logic
│   └── yenc/              # yEnc encoding/decoding
├── pkg/                   # Public packages
│   └── models/            # Data models and structures
├── main.go                # Application entry point
├── config.yaml.example    # Sample configuration
├── Makefile              # Build commands
└── README.md             # Complete documentation
```

## 🚀 Quick Start

### Prerequisites

- Go 1.19 or later
- Access to a Usenet provider/NNTP server
- PAR2 command-line tool (for PAR2 recovery file generation)

### Installation

1. Clone the repository:
```bash
git clone https://github.com/ahallais/yPost.git
cd yPost
```

2. Build the application:
```bash
go mod tidy
go build -o ypost main.go
```

Specific platform build

```bash
GOOS=linux GOARCH=amd64 go build -o ypost-linux-amd64 main.go
GOOS=darwin GOARCH=amd64 go build -o ypost-darwin-amd64 main.go
GOOS=windows GOARCH=amd64 go build -o ypost-windows-amd64.exe main.go
```

### Configuration

1. Initialize configuration:
```bash
./ypost config init
```

2. Edit the generated `config.yaml` file with your NNTP server details:
```yaml
nntp:
  server: "your-newsserver.com"
  port: 563
  username: "your-username"
  password: "your-password"
  ssl: true
  connections: 8

posting:
  newsgroup: "alt.binaries.test"
  from: "poster@example.com"
  subject_template: "[{{.Index}}/{{.Total}}] - {{.Filename}} - ({{.Size}})"

splitting:
  max_file_size: "50MB"
  max_lines: 5000

par2:
  redundancy: 10
  enabled: true

sfv:
  enabled: true

logging:
  level: "info"
  file: "ypost.log"
```

## 📖 Usage

### Basic File Posting

Post a single file:
```bash
./ypost post /path/to/your/file.iso
```



### Flags

| Flag                 | Type    | Description                               | Default                |
|----------------------|---------|-------------------------------------------|------------------------|
| `-g, --group`        | string  | Newsgroup to post to (e.g., `alt.binaries.multimedia`) | *none*                 |
| `--poster-name`      | string  | Name of the poster                        | *none*                 |
| `--poster-email`     | string  | Email address of the poster               | *none*                 |
| `-s, --subject`      | string  | Subject template for the post             | *none*                 |
| `--max-part-size`    | int     | Maximum size per part in bytes            | 768000 (750 KB)        |
| `--max-line-length`  | int     | Maximum line length                        | 128                    |
| `--par2`             | bool    | Create PAR2 recovery files                 | true                   |
| `--sfv`              | bool    | Create SFV checksum file                    | true                   |
| `--redundancy`       | int     | PAR2 redundancy percentage                  | 10                     |
| `-o, --output`       | string  | Output directory                           | *none*                 |
| `--nzb-dir`          | string  | NZB output directory                       | *none*                 |

---

### Example: Post with Custom Options

Post a file to a specific newsgroup with a custom subject:

```bash
./ypost post /path/to/your/file.iso --group "alt.binaries.multimedia"
```

## 🔧 Configuration Options

### NNTP Settings
- `server`: NNTP server hostname
- `port`: Server port (typically 119 for plain, 563 for SSL)
- `username`/`password`: Authentication credentials
- `ssl`: Enable SSL/TLS connection
- `connections`: Number of concurrent connections

### Posting Settings
- `newsgroup`: Default newsgroup for posting
- `from`: Email address in the From header
- `subject_template`: Template for post subjects

### File Processing
- `max_file_size`: Maximum size before splitting (e.g., "50MB", "100MB")
- `max_lines`: Maximum lines per post part
- `redundancy`: PAR2 redundancy percentage (5-50)


## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request. For major changes, please open an issue first to discuss what you would like to change.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🐛 Issues and Support

If you encounter any issues or need support:

1. Check the [Issues](https://github.com/ahallais/yPost/issues) page for existing problems
2. Create a new issue with detailed information about your problem
3. Include relevant log output and configuration (remove sensitive information)

## 📊 Logging

The application provides comprehensive logging with configurable levels:

- **Debug**: Detailed information for debugging
- **Info**: General operational messages
- **Warn**: Warning messages for potential issues
- **Error**: Error messages for failures

Logs are written to both console and file (configurable in `config.yaml`).

## 🔐 Security Notes

- Store your NNTP credentials securely
- Use environment variables for sensitive configuration
- Enable SSL/TLS when available
- Regularly update the application for security patches

---

**Ready to use immediately** - Build, configure, and start posting to Usenet with full PAR2 and SFV support!



## TODO

Post multiple files:
```bash
./ypost post /path/to/file1.zip /path/to/file2.rar
```

### Configuration Management

View current configuration:
```bash
./ypost config show
```

Update configuration values:
```bash
./ypost config set nntp.connections 12
./ypost config set posting.newsgroup "alt.binaries.movies"
```

### Advanced Options

Enable verbose logging:
```bash
./ypost post --verbose /path/to/file.iso
```

Dry run (test without posting):
```bash
./ypost post --dry-run /path/to/file.iso
```

Custom output directory:
```bash
./ypost post --output-dir /path/to/output /path/to/file.iso
```