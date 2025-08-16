package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	group          string
	posterName     string
	posterEmail    string
	subject        string
	maxPartSize    int64
	maxArticleSize int64
	maxLineLen     int
	createPAR2     bool
	createSFV      bool
	redundancy     int
	outputDir      string
	nzbDir         string
)

// postCmd represents the post command
var postCmd = &cobra.Command{
	Use:   "post [file]",
	Short: "Post a file to Usenet",
	Long: `Post a file to Usenet with automatic yEnc encoding, file splitting,
NZB generation, and optional PAR2/SFV creation.`,
	Args: cobra.ExactArgs(1),
	Run:  runPost,
}

func init() {
	rootCmd.AddCommand(postCmd)

	postCmd.Flags().StringVarP(&group, "group", "g", "", "newsgroup to post to")
	postCmd.Flags().StringVar(&posterName, "poster-name", "", "name of the poster")
	postCmd.Flags().StringVar(&posterEmail, "poster-email", "", "email address of the poster")
	postCmd.Flags().StringVarP(&subject, "subject", "s", "", "subject template")
	postCmd.Flags().Int64Var(&maxPartSize, "max-part-size", 0, "maximum size per part in bytes")
	postCmd.Flags().Int64Var(&maxArticleSize, "max-article-size", 0, "maximum size per NNTP article in bytes")
	postCmd.Flags().IntVar(&maxLineLen, "max-line-length", 128, "maximum line length")
	postCmd.Flags().BoolVar(&createPAR2, "par2", true, "create PAR2 recovery files")
	postCmd.Flags().BoolVar(&createSFV, "sfv", true, "create SFV checksum file")
	postCmd.Flags().IntVar(&redundancy, "redundancy", 10, "PAR2 redundancy percentage")
	postCmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory")
	postCmd.Flags().StringVar(&nzbDir, "nzb-dir", "", "NZB output directory")
}

func runPost(cmd *cobra.Command, args []string) {
	filePath := args[0]

	// Load configuration
	cfg, configFileUsed, err := config.LoadConfig(cfgFile)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Override config with command line flags
	if group != "" {
		cfg.Posting.Group = group
	}
	if posterName != "" {
		cfg.Posting.PosterName = posterName
	}
	if posterEmail != "" {
		cfg.Posting.PosterEmail = posterEmail
	}
	if subject != "" {
		cfg.Posting.SubjectTemplate = subject
	}
	if maxPartSize > 0 {
		cfg.Posting.MaxPartSize = maxPartSize
	}
	if maxArticleSize > 0 {
		cfg.Posting.MaxArticleSize = maxArticleSize
	}
	if maxLineLen > 0 {
		cfg.Posting.MaxLineLength = maxLineLen
	}
	if outputDir != "" {
		cfg.Output.OutputDir = outputDir
	}
	if nzbDir != "" {
		cfg.Output.NZBDir = nzbDir
	}

	// Initialize logger
	log, err := logger.New(cfg.Output.LogDir)
	if err != nil {
		fmt.Printf("Error initializing logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Close()

	// Log configuration file path and contents
	if configFileUsed != "" {
		// Get absolute path
		absPath, err := filepath.Abs(configFileUsed)
		if err != nil {
			absPath = configFileUsed
		}
		log.Info("Configuration file loaded: %s", absPath)
		
		// Read and log config file contents
		content, err := os.ReadFile(configFileUsed)
		if err == nil {
			log.Debug("Config contents: %s", string(content))
		} else {
			log.Warn("Could not read config file contents: %v", err)
		}
	} else {
		log.Info("Using default configuration (no config file found)")
	}

// Check if file exists
if _, err := os.Stat(filePath); os.IsNotExist(err) {
	log.Fatal("File does not exist: %s", filePath)
}

// Create unified output directory with timestamp
baseName := filepath.Base(filePath)
unifiedOutputDir := utils.GetUnifiedOutputPath(cfg.Output.OutputDir, baseName)

// Ensure the unified directory exists (even if some file types are disabled)
if err := os.MkdirAll(unifiedOutputDir, 0755); err != nil {
	log.Fatal("Failed to create unified output directory: %v", err)
}

// Initialize components
fmt.Printf("DEBUG: Initializing splitter with MaxPartSize: %d bytes\n", cfg.Posting.MaxPartSize)
split := splitter.NewSplitter(cfg.Posting.MaxPartSize)
yencEnc := yenc.Encoder{}

// Use the "from" value from config for NZB poster
poster := cfg.Posting.From
if poster == "" {
	// Fallback to poster_email if "from" is not specified
	poster = cfg.Posting.PosterEmail
}
nzbGen := nzb.NewGenerator(unifiedOutputDir, poster)

var par2Gen *par2.Generator
var sfvGen *sfv.Generator

if createPAR2 || cfg.Features.CreatePAR2 {
	par2Gen = par2.NewGenerator(unifiedOutputDir)
}
if createSFV || cfg.Features.CreateSFV {
	sfvGen = sfv.NewGenerator(unifiedOutputDir)
}

	// Split file into parts and save them to the output directory
	log.Info("Splitting file: %s", filePath)
	parts, err := split.SplitFile(filePath, unifiedOutputDir)
	if err != nil {
		log.Fatal("Failed to split file: %v", err)
	}

	log.LogFileSplit(filePath, len(parts), sumPartSizes(parts))

	// Create PAR2 files if enabled - use split parts for standard practice
	var par2Files []string
	if par2Gen != nil {
		log.Info("Creating PAR2 recovery files...")
		
		// Collect part file paths for PAR2 generation
		var partPaths []string
		for _, part := range parts {
			partPaths = append(partPaths, part.FilePath)
		}
		
		// Create PAR2 files for the split parts (standard practice)
		par2Files, err = par2Gen.CreatePAR2ForParts(partPaths, filepath.Base(filePath), redundancy)
		if err != nil {
			log.Error("Failed to create PAR2 files: %v", err)
		} else {
			log.LogPAR2Creation(filePath, par2Files)
		}
	}

	// Create SFV file for split parts if enabled
	var sfvPath string
	if sfvGen != nil {
		log.Info("Creating SFV checksum file...")
		
		// Collect paths of all files to include in SFV
		var allFilePaths []string
		
		// Add the split part files (standard practice)
		for _, part := range parts {
			allFilePaths = append(allFilePaths, part.FilePath)
		}
		
		// Add PAR2 files
		allFilePaths = append(allFilePaths, par2Files...)
		
		sfvPath, err = sfvGen.CreateSFV(allFilePaths, fmt.Sprintf("%s.sfv", filepath.Base(filePath)))
		if err != nil {
			log.Error("Failed to create SFV file: %v", err)
		} else {
			log.LogSFVCreation(filePath, sfvPath)
		}
	}

	// Initialize NNTP connection pool
	var allSegments []*models.PostSegment
	var pool *nntp.ConnectionPool
	
	for _, server := range cfg.NNTP.Servers {
		log.Info("Connecting to server: %s", server.Host)
		pool = nntp.NewConnectionPool(&server, server.MaxConns)
		
		// Upload parts
		segments, err := uploadParts(pool, parts, *cfg, &yencEnc, log)
		if err != nil {
			log.Error("Failed to upload parts: %v", err)
			pool.CloseAll()
			continue
		}
		
		allSegments = append(allSegments, segments...)
		break // Use first successful server
	}

	if len(allSegments) == 0 {
		if pool != nil {
			pool.CloseAll()
		}
		log.Fatal("Failed to upload any parts")
	}

	// Post PAR2 files if created
	var par2Segments []*models.PostSegment
	if len(par2Files) > 0 {
		log.Info("Posting PAR2 recovery files...")
		for _, par2File := range par2Files {
			par2Parts, err := split.SplitFile(par2File, unifiedOutputDir)
			if err != nil {
				log.Error("Failed to split PAR2 file: %v", err)
				continue
			}

			par2FileSegments, err := uploadParts(pool, par2Parts, *cfg, &yencEnc, log)
			if err != nil {
				log.Error("Failed to upload PAR2 parts: %v", err)
				continue
			}

			par2Segments = append(par2Segments, par2FileSegments...)
		}
	}

	// Post SFV file if created
	var sfvSegments []*models.PostSegment
	if sfvPath != "" {
		log.Info("Posting SFV checksum file...")
		sfvParts, err := split.SplitFile(sfvPath, unifiedOutputDir)
		if err != nil {
			log.Error("Failed to split SFV file: %v", err)
		} else {
			sfvFileSegments, err := uploadParts(pool, sfvParts, *cfg, &yencEnc, log)
			if err != nil {
				log.Error("Failed to upload SFV parts: %v", err)
			} else {
				sfvSegments = sfvFileSegments
			}
		}
	}

	// Close the connection pool when done
	if pool != nil {
		pool.CloseAll()
	}

	// Collect all additional files for NZB
	additionalFiles := make(map[string][]*models.PostSegment)
	if len(par2Segments) > 0 {
		additionalFiles["PAR2"] = par2Segments
	}
	if len(sfvSegments) > 0 {
		additionalFiles["SFV"] = sfvSegments
	}

	// Generate NZB file with all segments including PAR2 and SFV
	log.Info("Generating NZB file...")
	nzbPath, err := nzbGen.Generate(filepath.Base(filePath), allSegments, cfg.Posting.Group, additionalFiles)
	if err != nil {
		log.Fatal("Failed to generate NZB file: %v", err)
	}
	log.LogNZBCreation(filePath, nzbPath)

	// Move PAR2 and SFV files to the same directory as NZB
	if err := moveGeneratedFiles(par2Files, sfvPath, filepath.Dir(nzbPath)); err != nil {
		log.Error("Failed to move generated files: %v", err)
	} else {
		log.Info("Successfully moved PAR2 and SFV files to NZB directory")
	}

	// Clean up temporary part files
	log.Info("Cleaning up temporary files...")
	if err := cleanupAllPartFiles(split, parts, par2Segments, sfvSegments); err != nil {
		log.Error("Failed to clean up some temporary files: %v", err)
	}

	log.Info("Posting completed successfully!")
	log.Info("NZB file: %s", nzbPath)
}

// cleanupAllPartFiles removes all temporary part files
func cleanupAllPartFiles(split *splitter.Splitter, mainParts []*models.FilePart, par2Segments, sfvSegments []*models.PostSegment) error {
	var errors []error

	// Clean up main file parts
	if err := split.CleanupPartFiles(mainParts); err != nil {
		errors = append(errors, err)
	}

	// We don't have direct access to the PAR2 and SFV parts, but we can extract the file paths
	// from the segments and clean them up separately if needed

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during cleanup", len(errors))
	}
	return nil
}

// uploadJob represents a single chunk upload task
type uploadJob struct {
	chunkData   []byte
	part        *models.FilePart
	chunkIndex  int
	chunkNumber int
	totalParts  int
	totalChunks int
	totalBytes  int64
}

func uploadParts(pool *nntp.ConnectionPool, parts []*models.FilePart, postingConfig models.Config, yencEnc *yenc.Encoder, log *logger.Logger) ([]*models.PostSegment, error) {
	// Calculate total bytes for progress tracking
	var totalBytes int64
	for _, part := range parts {
		totalBytes += part.Size
	}
	
	// NNTP article size limit from configuration
	maxArticleSize := int(postingConfig.Posting.MaxArticleSize)
	
	// Calculate total chunks across all parts for proper numbering
	var totalChunks int
	var allJobs []uploadJob
	
	chunkNumber := 1
	
	// Prepare all upload jobs
	for _, part := range parts {
		data, err := os.ReadFile(part.FilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read part file %s: %w", part.FilePath, err)
		}
		
		chunks := splitDataIntoChunks(data, maxArticleSize)
		totalChunks += len(chunks)
		
		for chunkIndex, chunkData := range chunks {
			job := uploadJob{
				chunkData:   chunkData,
				part:        part,
				chunkIndex:  chunkIndex,
				chunkNumber: chunkNumber,
				totalParts:  len(parts),
				totalChunks: totalChunks, // Will be updated after we know the final count
				totalBytes:  totalBytes,
			}
			allJobs = append(allJobs, job)
			chunkNumber++
		}
	}
	
	// Update totalChunks in all jobs now that we know the final count
	for i := range allJobs {
		allJobs[i].totalChunks = totalChunks
	}
	
	// Create progress tracker
	tracker := progress.NewTracker(parts[0].FileName, totalChunks, totalBytes)
	
	// Create channels for work distribution and result collection
	jobs := make(chan uploadJob, len(allJobs))
	results := make(chan *models.PostSegment, len(allJobs))
	errors := make(chan error, len(allJobs))
	
	// Determine number of workers (use connection count from config)
	numWorkers := 4 // Default to 4 connections
	if len(postingConfig.NNTP.Servers) > 0 {
		numWorkers = postingConfig.NNTP.Servers[0].MaxConns
	}
	
	log.Info("Starting parallel upload with %d workers for %d chunks", numWorkers, totalChunks)
	
	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for job := range jobs {
				segment, err := uploadChunk(pool, job, postingConfig, yencEnc, log, tracker)
				if err != nil {
					log.Error("Worker %d failed to upload chunk %d: %v", workerID, job.chunkNumber, err)
					errors <- fmt.Errorf("worker %d: %w", workerID, err)
					return
				}
				results <- segment
			}
		}(i)
	}
	
	// Send all jobs to workers
	go func() {
		defer close(jobs)
		for _, job := range allJobs {
			jobs <- job
		}
	}()
	
	// Collect results
	var segments []*models.PostSegment
	var uploadErrors []error
	
	for i := 0; i < len(allJobs); i++ {
		select {
		case segment := <-results:
			segments = append(segments, segment)
		case err := <-errors:
			uploadErrors = append(uploadErrors, err)
		}
	}
	
	// Wait for all workers to complete
	wg.Wait()
	
	// Check for errors
	if len(uploadErrors) > 0 {
		return nil, fmt.Errorf("upload failed with %d errors: %v", len(uploadErrors), uploadErrors[0])
	}
	
	// Emit completion message
	tracker.EmitComplete()
	
	log.Info("Successfully uploaded %d chunks using %d parallel connections", len(segments), numWorkers)
	
	return segments, nil
}

// uploadChunk handles uploading a single chunk
func uploadChunk(pool *nntp.ConnectionPool, job uploadJob, postingConfig models.Config, yencEnc *yenc.Encoder, log *logger.Logger, tracker *progress.Tracker) (*models.PostSegment, error) {
	client, err := pool.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Join group
	if err := client.JoinGroup(postingConfig.Posting.Group); err != nil {
		return nil, fmt.Errorf("failed to join group: %w", err)
	}

	// Encode chunk with proper part information
	encoded := yencEnc.Encode(job.chunkData, job.part.FileName, job.part.PartNumber, job.totalParts)
	
	// Create subject using proper Go template processing
	subject := postingConfig.Posting.SubjectTemplate
	if subject == "" {
		subject = "[{{.Index}}/{{.Total}}] - {{.Filename}} - ({{.Size}}) yEnc ({{.ChunkIndex}}/{{.TotalChunks}})"
	}
	
	// Calculate file size in human-readable format
	fileSize := float64(job.totalBytes)
	sizeStr := ""
	if fileSize >= 1024*1024*1024 {
		sizeStr = fmt.Sprintf("%.1fGB", fileSize/(1024*1024*1024))
	} else if fileSize >= 1024*1024 {
		sizeStr = fmt.Sprintf("%.1fMB", fileSize/(1024*1024))
	} else if fileSize >= 1024 {
		sizeStr = fmt.Sprintf("%.1fKB", fileSize/1024)
	} else {
		sizeStr = fmt.Sprintf("%dB", int(fileSize))
	}
	
	// Create template data with both part and chunk information
	templateData := struct {
		Index       int    // Part number (for file parts like RAR)
		Total       int    // Total parts
		Filename    string
		Size        string
		ChunkIndex  int    // Chunk number (for NNTP articles)
		TotalChunks int    // Total chunks
	}{
		Index:       job.part.PartNumber,
		Total:       job.totalParts,
		Filename:    job.part.FileName,
		Size:        sizeStr,
		ChunkIndex:  job.chunkNumber,
		TotalChunks: job.totalChunks,
	}
	
	// Process template
	tmpl, err := template.New("subject").Parse(subject)
	if err != nil {
		// Fallback to format showing both part and chunk info
		subject = fmt.Sprintf("(%02d/%02d) - %s - (%s) yEnc (%04d/%04d)",
			job.part.PartNumber, job.totalParts, job.part.FileName, sizeStr, job.chunkNumber, job.totalChunks)
	} else {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, templateData); err != nil {
			// Fallback to format showing both part and chunk info
			subject = fmt.Sprintf("(%02d/%02d) - %s - (%s) yEnc (%04d/%04d)",
				job.part.PartNumber, job.totalParts, job.part.FileName, sizeStr, job.chunkNumber, job.totalChunks)
		} else {
			subject = buf.String()
		}
	}

	// Upload chunk
	messageID, err := client.PostArticle(
		postingConfig.Posting.Group,
		subject,
		fmt.Sprintf("%s <%s>", postingConfig.Posting.PosterName, postingConfig.Posting.PosterEmail),
		encoded,
		postingConfig.Posting.CustomHeaders,
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to post chunk %d of part %d: %w", job.chunkIndex+1, job.part.PartNumber, err)
	}

	segment := &models.PostSegment{
		MessageID:   messageID,
		PartNumber:  job.chunkNumber, // Use chunk number for NZB
		TotalParts:  job.totalChunks, // Total chunks for NZB
		FileName:    job.part.FileName,
		Subject:     subject,
		PostedAt:    time.Now(),
		BytesPosted: int64(len(job.chunkData)),
	}
	
	// Emit real-time progress (thread-safe)
	tracker.EmitProgress(job.chunkNumber, int64(len(job.chunkData)))
	
	log.LogUploadProgress(job.part.FileName, job.chunkNumber, job.totalChunks, int64(len(job.chunkData)))
	
	return segment, nil
}

// splitDataIntoChunks splits data into chunks of specified maximum size
func splitDataIntoChunks(data []byte, maxChunkSize int) [][]byte {
	var chunks [][]byte
	
	for i := 0; i < len(data); i += maxChunkSize {
		end := i + maxChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[i:end])
	}
	
	return chunks
}

func sumPartSizes(parts []*models.FilePart) int64 {
	var total int64
	for _, part := range parts {
		total += part.Size
	}
	return total
}

// moveGeneratedFiles moves PAR2 and SFV files to the NZB directory
func moveGeneratedFiles(par2Files []string, sfvPath string, nzbDir string) error {
	// Move PAR2 files
	for _, par2File := range par2Files {
		if _, err := os.Stat(par2File); err == nil {
			destPath := filepath.Join(nzbDir, filepath.Base(par2File))
			if err := os.Rename(par2File, destPath); err != nil {
				return fmt.Errorf("failed to move PAR2 file %s: %w", par2File, err)
			}
		}
	}

	// Move SFV file
	if sfvPath != "" {
		if _, err := os.Stat(sfvPath); err == nil {
			destPath := filepath.Join(nzbDir, filepath.Base(sfvPath))
			if err := os.Rename(sfvPath, destPath); err != nil {
				return fmt.Errorf("failed to move SFV file %s: %w", sfvPath, err)
			}
		}
	}

	return nil
}