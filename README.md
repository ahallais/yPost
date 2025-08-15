# Usenet Poster


Disclaimer: This was done with Vibe Coding. I'm not familiar with Go, so I relied entirely on vibe coding here. Any feedback or improvements are welcome!


Go Command-line tool for posting binart files to Usenet newsgroups

## ✨ Features

- **yEnc Encoding** 
- **File Splitting**
- **NZB Generation**
- **PAR2 Recovery Files** 
- **SFV Checksum Files**
- **NNTP Client**
- **Configuration Management** - YAML-based configuration with environment variable support
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

 `config.yaml` file with your NNTP server details:

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

---


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