package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
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
	createPAR2, createSFV, keepTempFiles    bool
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
	f.BoolVar(&keepTempFiles, "keep-temp-files", false, "keep split, SFV, and PAR2 files after a successful upload")
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
			_ = nzbGen.Abort()
		}
	}()

	// Small metadata first, then data, then recovery volumes. They remain one
	// posting session and one NZB/release; PAR2 is not a separate release.
	files := postingOrder(sfvPath, par2Files, partPaths)
	server := cfg.NNTP.Servers[0]
	configuredConnections := server.MaxConns
	connections, workload, err := workloadAwareConnectionLimit(server.MaxConns, cfg.Posting.MaxArticleSize,
		cfg.Posting.TargetBytesPerConnection, files)
	if err != nil {
		return fmt.Errorf("size upload workload: %w", err)
	}
	server.MaxConns = connections
	log.Info("NNTP workload uses %d of %d configured connections for %s across %d articles",
		server.MaxConns, configuredConnections, formatMemory(workload.Bytes), workload.Articles)

	log.Info("[3/5] Connecting to Usenet")
	// PAR2 generation can leave a large idle heap behind. Return it before
	// measuring available memory and allocating upload worker buffers.
	debug.FreeOSMemory()
	if memoryAvailable, memoryErr := availableMemory(); memoryErr == nil {
		connections, perWorker := memoryAwareConnectionLimit(server.MaxConns, cfg.Posting.MaxArticleSize, memoryAvailable)
		if connections < server.MaxConns {
			log.Warn("Reducing NNTP connections from %d to %d: %s available memory, estimated %s per worker",
				server.MaxConns, connections, formatMemory(memoryAvailable), formatMemory(perWorker))
		} else {
			log.Info("Using %d NNTP connections with %s available memory (estimated %s per worker)",
				connections, formatMemory(memoryAvailable), formatMemory(perWorker))
		}
		server.MaxConns = connections
	} else {
		log.Warn("Available memory could not be detected; using workload-selected %d NNTP connections: %v", server.MaxConns, memoryErr)
	}
	log.Info("Opening and authenticating up to %d NNTP connections serially; posting starts after the first is ready", server.MaxConns)
	pool := nntp.NewConnectionPool(&server, server.MaxConns)
	connectionProgress := progress.NewCounterTracker("NNTP connections", int64(server.MaxConns))
	var connected atomic.Int64
	connectCtx, cancelConnections := context.WithCancel(context.Background())
	connectionResults := pool.ConnectSequentially(connectCtx, func(completed, _ int) {
		completed64 := int64(completed)
		previous := connected.Swap(completed64)
		if completed64 > previous {
			connectionProgress.Add(completed64 - previous)
		}
	})
	posterArrivals := make(chan posterArrival, server.MaxConns)
	go func() {
		defer close(posterArrivals)
		worker := 0
		for result := range connectionResults {
			if result.Err != nil {
				posterArrivals <- posterArrival{err: result.Err}
				return
			}
			worker++
			workerNumber := worker
			result.Client.SetRetryHook(func(event nntp.RetryEvent) {
				if event.Delay > 0 {
					log.Warn("NNTP worker %d: %s; retry %d/%d in %s: %v",
						workerNumber, event.Kind, event.Attempt, event.Maximum, event.Delay, event.Err)
					return
				}
				log.Warn("NNTP worker %d: %s; retry %d/%d: %v",
					workerNumber, event.Kind, event.Attempt, event.Maximum, event.Err)
			})
			posterArrivals <- posterArrival{poster: result.Client}
		}
	}()
	defer func() {
		cancelConnections()
		for range posterArrivals {
		}
		pool.CloseAll()
	}()

	first, ok := <-posterArrivals
	if !ok {
		return fmt.Errorf("connect NNTP workers: no connection result")
	}
	if first.err != nil {
		return fmt.Errorf("connect NNTP workers: %w", first.err)
	}
	posters := []articlePoster{first.poster}
	log.Info("[4/5] Posting %d release files", len(files))
	uploadProgress := progress.NewStageTracker("Upload", workload.Bytes)
	for _, path := range files {
		posters, err = postNyuuFileWithDynamicPosters(posters, posterArrivals, path, cfg, nzbGen, log, uploadProgress.Add)
		if err != nil {
			return err
		}
	}
	uploadProgress.Finish()
	cancelConnections()

	log.Info("[5/5] Finalizing NZB")
	if err := nzbGen.Close(); err != nil {
		return fmt.Errorf("finalize NZB: %w", err)
	}
	closed = true
	if !cfg.Output.KeepTempFiles {
		artifacts := make([]string, 0, len(partPaths)+len(par2Files)+1)
		artifacts = append(artifacts, partPaths...)
		if sfvPath != "" {
			artifacts = append(artifacts, sfvPath)
		}
		artifacts = append(artifacts, par2Files...)
		if err := cleanupUploadArtifacts(artifacts); err != nil {
			return fmt.Errorf("clean up temporary upload files (NZB retained at %s): %w", nzbPath, err)
		}
		// Remove the per-upload working directory when cleanup left it empty.
		// Ignore a non-empty directory, including when it contains the NZB.
		_ = os.Remove(workDir)
	}
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
	if f.Changed("keep-temp-files") {
		cfg.Output.KeepTempFiles = keepTempFiles
	}
}

func cleanupUploadArtifacts(paths []string) error {
	removed := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		cleanPath := filepath.Clean(path)
		if _, exists := removed[cleanPath]; exists {
			continue
		}
		removed[cleanPath] = struct{}{}
		if err := os.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", cleanPath, err)
		}
	}
	return nil
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

type articlePoster interface {
	PostArticleStreamContext(context.Context, string, string, string, nntp.BodyWriter, map[string]string) (string, error)
}

type posterArrival struct {
	poster articlePoster
	err    error
}

type articleTask struct {
	part   int
	offset int64
	size   int
}

type articleResult struct {
	part      int
	size      int64
	messageID string
	err       error
}

func postNyuuFile(clients []*nntp.Client, path string, cfg *models.Config, nzbGen *nzb.NyuuGenerator, log *logger.Logger, uploaded func(int64)) error {
	posters := make([]articlePoster, len(clients))
	for i, client := range clients {
		posters[i] = client
	}
	return postNyuuFileWithPosters(posters, path, cfg, nzbGen, log, uploaded)
}

func postNyuuFileWithPosters(posters []articlePoster, path string, cfg *models.Config, nzbGen *nzb.NyuuGenerator, log *logger.Logger, uploaded func(int64)) error {
	arrivals := make(chan posterArrival)
	close(arrivals)
	_, err := postNyuuFileWithDynamicPosters(posters, arrivals, path, cfg, nzbGen, log, uploaded)
	return err
}

// postNyuuFileWithDynamicPosters posts one file while allowing newly
// authenticated connections to join its worker set. It returns every poster
// acquired so they can be reused by the next sequential file.
func postNyuuFileWithDynamicPosters(posters []articlePoster, arrivals <-chan posterArrival, path string, cfg *models.Config, nzbGen *nzb.NyuuGenerator, log *logger.Logger, uploaded func(int64)) ([]articlePoster, error) {
	if len(posters) == 0 {
		return posters, fmt.Errorf("post %s: no NNTP workers available", filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return posters, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return posters, fmt.Errorf("stat %s: %w", path, err)
	}
	fileSize := info.Size()
	name := filepath.Base(path)
	articleSize := cfg.Posting.MaxArticleSize
	if articleSize <= 0 {
		return posters, fmt.Errorf("post %s: article size must be positive", name)
	}
	maxInt := int64(^uint(0) >> 1)
	if articleSize > maxInt && fileSize > maxInt {
		return posters, fmt.Errorf("post %s: article size exceeds platform limit", name)
	}
	totalParts := 0
	if fileSize > 0 {
		partCount := (fileSize-1)/articleSize + 1
		if partCount > maxInt {
			return posters, fmt.Errorf("post %s: article count exceeds platform limit", name)
		}
		totalParts = int(partCount)
	}
	groups := strings.Split(cfg.Posting.Group, ",")
	for i := range groups {
		groups[i] = strings.TrimSpace(groups[i])
	}
	if err := nzbGen.StartFile(name, groups, time.Now()); err != nil {
		return posters, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan articleTask)
	results := make(chan articleResult, len(posters)+64)
	var workers sync.WaitGroup
	sendResult := func(result articleResult) bool {
		select {
		case results <- result:
			return true
		case <-ctx.Done():
			return false
		}
	}
	startWorker := func(poster articlePoster) {
		workers.Add(1)
		go func(poster articlePoster) {
			defer workers.Done()
			encoder := yenc.NewEncoder(cfg.Posting.MaxLineLength)
			var buffer []byte
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-jobs:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						return
					}
					if cap(buffer) < task.size {
						buffer = make([]byte, task.size)
					}
					chunk := buffer[:task.size]
					n, err := file.ReadAt(chunk, task.offset)
					if err != nil && err != io.EOF {
						sendResult(articleResult{part: task.part, err: fmt.Errorf("read article: %w", err)})
						cancel()
						return
					}
					if n != len(chunk) {
						sendResult(articleResult{part: task.part, err: fmt.Errorf("read article: %w", io.ErrUnexpectedEOF)})
						cancel()
						return
					}
					subj := renderSubject(cfg.Posting.SubjectTemplate, name, task.part, totalParts, fileSize)
					writeBody := func(writer io.Writer) error {
						return encoder.EncodePartTo(writer, chunk, name, task.part, totalParts, fileSize, task.offset+1)
					}
					messageID, err := poster.PostArticleStreamContext(ctx, cfg.Posting.Group, subj,
						fmt.Sprintf("%s <%s>", cfg.Posting.PosterName, cfg.Posting.PosterEmail), writeBody, cfg.Posting.CustomHeaders)
					if err != nil {
						sendResult(articleResult{part: task.part, err: err})
						cancel()
						return
					}
					if !sendResult(articleResult{part: task.part, size: int64(task.size), messageID: strings.Trim(messageID, "<>")}) {
						return
					}
				}
			}
		}(poster)
	}
	for _, poster := range posters {
		startWorker(poster)
	}

	go func() {
		defer close(jobs)
		for part := 1; part <= totalParts; part++ {
			offset := int64(part-1) * articleSize
			size := articleSize
			if remaining := fileSize - offset; remaining < size {
				size = remaining
			}
			select {
			case jobs <- articleTask{part: part, offset: offset, size: int(size)}:
			case <-ctx.Done():
				return
			}
		}
	}()
	ordered := make([]articleResult, totalParts)
	var firstErr error
	completed := 0
	activeArrivals := arrivals
	for completed < totalParts && firstErr == nil {
		select {
		case arrival, ok := <-activeArrivals:
			if !ok {
				activeArrivals = nil
				continue
			}
			if arrival.err != nil {
				firstErr = fmt.Errorf("connect additional NNTP worker: %w", arrival.err)
				cancel()
				continue
			}
			posters = append(posters, arrival.poster)
			startWorker(arrival.poster)
		case result := <-results:
			completed++
			if result.err != nil {
				firstErr = fmt.Errorf("post %s article %d/%d: %w", name, result.part, totalParts, result.err)
				cancel()
				continue
			}
			ordered[result.part-1] = result
			if uploaded != nil {
				uploaded(result.size)
			}
			log.LogUploadProgress(name, result.part, totalParts, result.size)
		}
	}
	workers.Wait()
	if firstErr != nil {
		return posters, firstErr
	}
	for _, result := range ordered {
		if err := nzbGen.AddSegment(result.messageID, result.size); err != nil {
			return posters, err
		}
	}
	return posters, nzbGen.EndFile()
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
