# yPost

yPost is a Go command-line tool that prepares and posts a binary release to
Usenet in one command. It does not create RAR, ZIP, or 7z archives. It splits
the input file, creates optional SFV and PAR2 recovery files, yEnc-encodes every
file, uploads the complete release, and writes one Nyuu-compatible NZB.

Only post files you are allowed to distribute, and follow your provider's
terms and the rules of the target newsgroup.

## Features

- Plain file splitting (no archive conversion)
- yEnc multipart posting over NNTP/SSL
- SFV checksums for the data splits
- PAR2 index and recovery volumes for the data splits
- One NZB containing metadata, data, and recovery files
- YAML configuration with explicit command-line overrides

PAR2 files are uploaded in the same posting session and included in the same
NZB. They are separate files, not a separate release. The upload order is the
small PAR2 index and SFV, data splits, then PAR2 recovery volumes.

## Build

Go 1.22 or later is required.

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ypost .
```

This produces a stripped, statically linked executable with no Node.js, libc,
or other runtime dependency.

## Configuration

By default yPost looks for `config.yaml` in the current directory, then
`$HOME/.ypost/config.yaml`, then `/etc/ypost/config.yaml`. Select another file
with `--config`.

```yaml
nntp:
  connect_timeout: 30s
  command_timeout: 30s
  post_timeout: 120s
  reconnect_delay: 15s
  request_retries: 5
  post_retries: 1
  servers:
    - host: "news.example.com"
      port: 563
      username: "username"
      password: "password"
      ssl: true
      max_connections: 8

posting:
  group: "alt.binaries.test"
  poster_name: "Poster"
  poster_email: "poster@example.invalid"
  from: "Poster <poster@example.invalid>"
  subject_template: '"{{.Filename}}" yEnc ({{.Index}}/{{.Total}})'
  max_part_size: 768000
  max_article_size: 768000
  target_bytes_per_connection: 6144000
  max_line_length: 128
  custom_headers: {}

# Physical .001/.002 files. Keep this much larger than an NNTP article.
splitting:
  max_file_size: "50MB"

par2:
  enabled: true
  redundancy: 10

sfv:
  enabled: true

output:
  output_dir: "output"
  nzb_dir: "output/nzb"
  log_dir: "output/logs"
  keep_temp_files: false

logging:
  level: "info"
  file: "ypost.log"
```

For compatibility, the legacy single-server `nntp.server` form and
`posting.newsgroup` are also accepted. The legacy `splitting.max_file_size`
setting, when present, takes precedence over `posting.max_part_size`.
For a server entry, `max_connections` is the ceiling for concurrent NNTP
article workers; legacy single-server configurations use `nntp.connections`.
Before connecting, yPost sizes the final SFV, PAR2, and data-file workload and
reduces the worker count when the upload is too small to justify every
configured connection. The default target is eight articles of work per
connection. Set `posting.target_bytes_per_connection` to override that target
in bytes; `0` or an omitted value selects the eight-article default.
Each worker owns one connection and buffers at most one
`posting.max_article_size` chunk, keeping upload memory bounded independently
of the physical split-file size. yEnc output streams directly into the bounded
network buffer instead of constructing a complete encoded article. If a
connection is reset or reaches EOF while posting, that worker reconnects and
retries the article up to `request_retries` times with the same Message-ID.
Code `441` responses use the independent `post_retries` limit. Explicit article
history and Message-ID duplicate rejections (such as `441 Already exists in
history`) are recognized as successful uploads since the article was already
accepted by the server. Retry attempts are logged, and a Message-ID returned by
the server replaces the submitted ID in the NZB. On Linux, yPost reads
`MemAvailable` before connecting and reduces
the requested connection count when the estimated yEnc worker memory would
consume more than half of available memory. Connections are still opened and
authenticated serially to avoid a TLS/CPU spike, but posting starts as soon as
the first connection is ready and later connections join the active file.

NZBs are written to a temporary file in the destination directory and
atomically renamed into place only after every article succeeds. Temporary NZB
output is removed when posting fails.

After a successful upload, generated split, SFV, and PAR2 files are removed by
default. The completed NZB is always retained. Set `output.keep_temp_files` to
`true`, or pass `--keep-temp-files`, to retain the generated upload files.

The subject template supports `Filename`, `Index`, `Total`, `ChunkIndex`,
`TotalChunks`, and `Size`, using Go template syntax as shown above.

## Usage

```bash
./ypost ./Myfile.7z
```

Every setting is read from YAML first. A flag overrides its corresponding
setting only when that flag is explicitly supplied:

| Flag | Type | Description | Config/default |
|---|---:|---|---|
| `-g, --group` | string | Newsgroup(s), comma separated | `posting.group` |
| `--poster-name` | string | Poster display name | `posting.poster_name` |
| `--poster-email` | string | Poster email address | `posting.poster_email` |
| `-s, --subject` | string | Subject template | `posting.subject_template` |
| `--max-part-size` | int | Maximum split size in bytes | `posting.max_part_size` / 768000 |
| `--max-line-length` | int | Maximum yEnc line length | `posting.max_line_length` / 128 |
| `--par2` | bool | Generate and post PAR2 files | `par2.enabled` / true |
| `--sfv` | bool | Generate and post an SFV file | `sfv.enabled` / true |
| `--redundancy` | int | PAR2 recovery percentage (0–100) | `par2.redundancy` / 10 |
| `-o, --output` | string | Working/output directory | `output.output_dir` |
| `--nzb-dir` | string | Final NZB directory | `output.nzb_dir` |
| `--keep-temp-files` | bool | Keep split, SFV, and PAR2 files after success | `output.keep_temp_files` / false |

Boolean features can be disabled explicitly:

```bash
./ypost movie.mkv --par2=false --sfv=false
```

Other examples:

```bash
./ypost --config ./private.yaml file.iso
./ypost file.iso --group alt.binaries.test --redundancy 15
./ypost --max-part-size 104857600 --nzb-dir ./nzb file.iso
```

Use a fake or throwaway From identity rather than a personal address. Test a
new configuration in an appropriate test group first, and verify the completed
upload with the generated NZB.

## License

MIT. See [LICENSE](LICENSE).
