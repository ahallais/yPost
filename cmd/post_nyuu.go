package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"
	"ypost/internal/config"
	"ypost/internal/logger"
	"ypost/internal/nntp"
	"ypost/internal/nzb"
	"ypost/internal/par2"
	"ypost/internal/progress"
	"ypost/internal/sfv"
	"ypost/internal/splitter"
	"ypost/internal/utils"
	"ypost/internal/yenc"
	"ypost/pkg/models"
)

var (
	group, posterName, posterEmail, subject string
	outputDir, nzbDir                       string
	maxPartSize                             int64
	maxLineLen, redundancy                  int
	createPAR2, createSFV                   bool
)

func init() {
	rootCmd.Use = "ypost [flags] FILE"
	rootCmd.Short = "Split, protect, and post a file to Usenet"
	rootCmd.Long = `Split a file without archiving it, create optional SFV and PAR2 files,
post the complete release with yEnc, and write one Nyuu-compatible NZB. Values
come from config.yaml and explicitly supplied flags override them.`
	rootCmd.Args = cobra.ExactArgs(1)
	rootCmd.RunE = runPostNyuu

	f := rootCmd.Flags()
	f.StringVarP(&group, "group", "g", "", "newsgroup to post to")
	f.StringVar(&posterName, "poster-name", "", "name of the poster")
	f.StringVar(&posterEmail, "poster-email", "", "email address of the poster")
	f.StringVarP(&subject, "subject", "s", "", "subject template")
	f.Int64Var(&maxPartSize, "max-part-size", 0, "maximum size per split part in bytes")
	f.IntVar(&maxLineLen, "max-line-length", 0, "maximum yEnc line length")
	f.BoolVar(&createPAR2, "par2", true, "create PAR2 recovery files")
	f.BoolVar(&createSFV, "sfv", true, "create SFV checksum file")
	f.IntVar(&redundancy, "redundancy", 0, "PAR2 redundancy percentage")
	f.StringVarP(&outputDir, "output", "o", "", "output directory")
	f.StringVar(&nzbDir, "nzb-dir", "", "NZB output directory")
}

func runPostNyuu(cmd *cobra.Command, args []string) error {
	cfg, configFileUsed, err := config.LoadConfig(cfgFile)
	if err != nil {
		return err
	}
	applyPostOverrides(cmd, cfg)
	if err := validatePostOptions(cfg); err != nil {
		return err
	}

	log, err := logger.New(cfg.Output.LogDir)
	if err != nil {
		return fmt.Errorf("initialize logger: %w", err)
	}
	defer log.Close()
	if configFileUsed != "" {
		log.Info("Configuration file loaded: %s", configFileUsed)
	}

	input := args[0]
	info, err := os.Stat(input)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input must be a regular file: %s", input)
	}
	if info.Size() == 0 {
		return fmt.Errorf("input file is empty: %s", input)
	}

	baseName := filepath.Base(input)
	workDir := utils.GetUnifiedOutputPath(cfg.Output.OutputDir, baseName)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	log.Info("[1/5] Splitting %s", baseName)
	split := splitter.NewSplitter(cfg.Posting.MaxPartSize)
	splitProgress := progress.NewStageTracker("Splitting", info.Size())
	parts, err := split.SplitFileWithProgress(input, workDir, splitProgress.Add)
	if err != nil {
		return fmt.Errorf("split input: %w", err)
	}
	splitProgress.Finish()
	partPaths := filePartPaths(parts)

	log.Info("[2/5] Creating verification and recovery files")
	var sfvPath string
	if cfg.SFV.Enabled {
		sfvProgress := progress.NewStageTracker("SFV", totalFileSize(partPaths))
		sfvPath, err = sfv.NewGenerator(workDir).CreateSFVWithProgress(partPaths, baseName+".sfv", sfvProgress.Add)
		if err != nil {
			return fmt.Errorf("create SFV: %w", err)
		}
		sfvProgress.Finish()
	}
	var par2Files []string
	if cfg.Par2.Enabled {
		parGenerator := par2.NewGenerator(workDir)
		var parProgress *progress.StageTracker
		var parPhase string
		var parCompleted int64
		parGenerator.SetProgressCallback(func(phase string, completed, total int64) {
			if phase != parPhase {
				if parProgress != nil {
					parProgress.Finish()
				}
				parPhase = phase
				parCompleted = 0
				parProgress = progress.NewCounterTracker(phase, total)
			}
			if completed > parCompleted {
				parProgress.Add(completed - parCompleted)
				parCompleted = completed
			}
		})
		par2Files, err = parGenerator.CreatePAR2ForParts(partPaths, baseName, cfg.Par2.Redundancy)
		if err != nil {
			return fmt.Errorf("create PAR2: %w", err)
		}
		if parProgress != nil {
			parProgress.Finish()
		}
	}

	nzbRoot := cfg.Output.NZBDir
	if nzbRoot == "" {
		nzbRoot = workDir
	}
	if err := os.MkdirAll(nzbRoot, 0755); err != nil {
		return fmt.Errorf("create NZB directory: %w", err)
	}
	nzbPath := filepath.Join(nzbRoot, baseName+".nzb")
	poster := cfg.Posting.From
	if poster == "" {
		poster = fmt.Sprintf("%s <%s>", cfg.Posting.PosterName, cfg.Posting.PosterEmail)
	}
	nzbGen, err := nzb.NewNyuuGenerator(nzbPath, poster)
	if err != nil {
		return fmt.Errorf("create NZB: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = nzbGen.Close()
		}
	}()

	log.Info("[3/5] Connecting to Usenet")
	server := cfg.NNTP.Servers[0]
	pool := nntp.NewConnectionPool(&server, server.MaxConns)
	defer pool.CloseAll()

	// Small metadata first, then data, then recovery volumes. They remain one
	// posting session and one NZB/release; PAR2 is not a separate release.
	files := postingOrder(sfvPath, par2Files, partPaths)
	log.Info("[4/5] Posting %d release files", len(files))
	uploadProgress := progress.NewStageTracker("Upload", totalFileSize(files))
	enc := yenc.NewEncoder(cfg.Posting.MaxLineLength)
	for _, path := range files {
		if err := postNyuuFile(pool, path, cfg, enc, nzbGen, log, uploadProgress.Add); err != nil {
			return err
		}
	}
	uploadProgress.Finish()

	log.Info("[5/5] Finalizing NZB")
	if err := nzbGen.Close(); err != nil {
		return fmt.Errorf("finalize NZB: %w", err)
	}
	closed = true
	log.Info("Posting completed successfully; NZB: %s", nzbPath)
	return nil
}

func validatePostOptions(cfg *models.Config) error {
	if strings.TrimSpace(cfg.Posting.Group) == "" {
		return fmt.Errorf("posting group cannot be empty")
	}
	if cfg.Posting.MaxPartSize <= 0 {
		return fmt.Errorf("max part size must be positive")
	}
	if cfg.Posting.MaxArticleSize <= 0 {
		return fmt.Errorf("posting.max_article_size must be positive")
	}
	if cfg.Posting.MaxLineLength <= 0 {
		return fmt.Errorf("max line length must be positive")
	}
	if cfg.Par2.Redundancy < 0 || cfg.Par2.Redundancy > 100 {
		return fmt.Errorf("PAR2 redundancy must be between 0 and 100")
	}
	return nil
}

func applyPostOverrides(cmd *cobra.Command, cfg *models.Config) {
	f := cmd.Flags()
	if f.Changed("group") {
		cfg.Posting.Group = group
	}
	if f.Changed("poster-name") {
		cfg.Posting.PosterName = posterName
	}
	if f.Changed("poster-email") {
		cfg.Posting.PosterEmail = posterEmail
	}
	if f.Changed("subject") {
		cfg.Posting.SubjectTemplate = subject
	}
	if f.Changed("max-part-size") {
		cfg.Posting.MaxPartSize = maxPartSize
	}
	if f.Changed("max-line-length") {
		cfg.Posting.MaxLineLength = maxLineLen
	}
	if f.Changed("par2") {
		cfg.Par2.Enabled = createPAR2
	}
	if f.Changed("sfv") {
		cfg.SFV.Enabled = createSFV
	}
	if f.Changed("redundancy") {
		cfg.Par2.Redundancy = redundancy
	}
	if f.Changed("output") {
		cfg.Output.OutputDir = outputDir
	}
	if f.Changed("nzb-dir") {
		cfg.Output.NZBDir = nzbDir
	}
}

func filePartPaths(parts []*models.FilePart) []string {
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		paths = append(paths, part.FilePath)
	}
	return paths
}

func postingOrder(sfvPath string, par2Files, dataFiles []string) []string {
	files := make([]string, 0, len(dataFiles)+len(par2Files)+1)
	if len(par2Files) > 0 {
		files = append(files, par2Files[0])
	}
	if sfvPath != "" {
		files = append(files, sfvPath)
	}
	files = append(files, dataFiles...)
	if len(par2Files) > 1 {
		files = append(files, par2Files[1:]...)
	}
	return files
}

func postNyuuFile(pool *nntp.ConnectionPool, path string, cfg *models.Config, enc *yenc.Encoder, nzbGen *nzb.NyuuGenerator, log *logger.Logger, onProgress func(int64)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	name := filepath.Base(path)
	articleSize := int(cfg.Posting.MaxArticleSize)
	chunks := splitDataIntoChunks(data, articleSize)
	groups := strings.Split(cfg.Posting.Group, ",")
	for i := range groups {
		groups[i] = strings.TrimSpace(groups[i])
	}
	if err := nzbGen.StartFile(name, groups, time.Now()); err != nil {
		return err
	}
	tracker := progress.NewTracker(name, len(chunks), int64(len(data)))
	var offset int64 = 1
	for i, chunk := range chunks {
		part := i + 1
		body := enc.EncodePart(chunk, name, part, len(chunks), int64(len(data)), offset)
		subj := renderSubject(cfg.Posting.SubjectTemplate, name, part, len(chunks), int64(len(data)))
		client, err := pool.GetClient()
		if err != nil {
			return fmt.Errorf("get NNTP client: %w", err)
		}
		messageID, err := client.PostArticle(cfg.Posting.Group, subj,
			fmt.Sprintf("%s <%s>", cfg.Posting.PosterName, cfg.Posting.PosterEmail), body, cfg.Posting.CustomHeaders)
		if err != nil {
			return fmt.Errorf("post %s article %d/%d: %w", name, part, len(chunks), err)
		}
		if err := nzbGen.AddSegment(strings.Trim(messageID, "<>"), int64(len(chunk))); err != nil {
			return err
		}
		tracker.EmitProgress(part, int64(len(chunk)))
		if onProgress != nil {
			onProgress(int64(len(chunk)))
		}
		log.LogUploadProgress(name, part, len(chunks), int64(len(chunk)))
		offset += int64(len(chunk))
	}
	tracker.EmitComplete()
	return nzbGen.EndFile()
}

func totalFileSize(paths []string) int64 {
	var total int64
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			total += info.Size()
		}
	}
	return total
}

func splitDataIntoChunks(data []byte, size int) [][]byte {
	chunks := make([][]byte, 0, (len(data)+size-1)/size)
	for start := 0; start < len(data); start += size {
		end := start + size
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[start:end])
	}
	return chunks
}

func renderSubject(pattern, name string, index, total int, size int64) string {
	if pattern == "" {
		pattern = `"{{.Filename}}" yEnc ({{.Index}}/{{.Total}})`
	}
	values := struct {
		Filename                              string
		Index, Total, ChunkIndex, TotalChunks int
		Size                                  string
	}{
		Filename: name, Index: index, Total: total, ChunkIndex: index, TotalChunks: total,
		Size: fmt.Sprintf("%dB", size),
	}
	t, err := template.New("subject").Parse(pattern)
	if err != nil {
		return fmt.Sprintf(`"%s" yEnc (%d/%d)`, name, index, total)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, values); err != nil {
		return fmt.Sprintf(`"%s" yEnc (%d/%d)`, name, index, total)
	}
	return b.String()
}
