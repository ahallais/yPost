package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"usenet-poster/internal/config"
	"usenet-poster/internal/logger"
	"usenet-poster/internal/nntp"
	"usenet-poster/internal/nzb"
	"usenet-poster/internal/par2"
	"usenet-poster/internal/sfv"
	"usenet-poster/internal/splitter"
	"usenet-poster/internal/yenc"
	"usenet-poster/pkg/models"
)

var (
	group        string
	posterName   string
	posterEmail  string
	subject      string
	maxPartSize  int64
	maxLineLen   int
	createPAR2   bool
	createSFV    bool
	redundancy   int
	outputDir    string
	nzbDir       string
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
	postCmd.Flags().Int64Var(&maxPartSize, "max-part-size", 750*1024, "maximum size per part in bytes")
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
	cfg, err := config.LoadConfig(cfgFile)
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

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Fatal("File does not exist: %s", filePath)
	}

	// Create output directories
	if err := os.MkdirAll(cfg.Output.OutputDir, 0755); err != nil {
		log.Fatal("Failed to create output directory: %v", err)
	}
	if err := os.MkdirAll(cfg.Output.NZBDir, 0755); err != nil {
		log.Fatal("Failed to create NZB directory: %v", err)
	}

	// Initialize components
	split := splitter.NewSplitter(cfg.Posting.MaxPartSize, cfg.Posting.MaxLineLength)
	yencEnc := yenc.Encoder{}
	nzbGen := nzb.NewGenerator(cfg.Output.NZBDir)
	
	var par2Gen *par2.Generator
	var sfvGen *sfv.Generator
	
	if createPAR2 || cfg.Features.CreatePAR2 {
		par2Gen = par2.NewGenerator(cfg.Output.OutputDir)
	}
	if createSFV || cfg.Features.CreateSFV {
		sfvGen = sfv.NewGenerator(cfg.Output.OutputDir)
	}

	// Split file into parts
	log.Info("Splitting file: %s", filePath)
	parts, err := split.SplitFile(filePath)
	if err != nil {
		log.Fatal("Failed to split file: %v", err)
	}

	log.LogFileSplit(filePath, len(parts), sumPartSizes(parts))

	// Create PAR2 files if enabled
	var par2Files []string
	if par2Gen != nil {
		log.Info("Creating PAR2 recovery files...")
		par2Files, err = par2Gen.CreatePAR2(filePath, redundancy)
		if err != nil {
			log.Error("Failed to create PAR2 files: %v", err)
		} else {
			log.LogPAR2Creation(filePath, par2Files)
		}
	}

	// Create SFV file if enabled
	var sfvPath string
	if sfvGen != nil {
		log.Info("Creating SFV checksum file...")
		filePaths := []string{filePath}
		if len(par2Files) > 0 {
			filePaths = append(filePaths, par2Files...)
		}
		
		sfvPath, err = sfvGen.CreateSFV(filePaths, fmt.Sprintf("%s.sfv", filepath.Base(filePath)))
		if err != nil {
			log.Error("Failed to create SFV file: %v", err)
		} else {
			log.LogSFVCreation(filePath, sfvPath)
		}
	}

	// Initialize NNTP connection pool
	var allSegments []*models.PostSegment
	
	for _, server := range cfg.NNTP.Servers {
		log.Info("Connecting to server: %s", server.Host)
		pool := nntp.NewConnectionPool(&server, server.MaxConns)
		
		// Upload parts
		segments, err := uploadParts(pool, parts, &cfg.Posting, &yencEnc, log)
		if err != nil {
			log.Error("Failed to upload parts: %v", err)
			continue
		}
		
		allSegments = append(allSegments, segments...)
		pool.CloseAll()
		break // Use first successful server
	}

	if len(allSegments) == 0 {
		log.Fatal("Failed to upload any parts")
	}

	// Generate NZB file
	log.Info("Generating NZB file...")
	nzbPath, err := nzbGen.Generate(filepath.Base(filePath), allSegments, cfg.Posting.Group)
	if err != nil {
		log.Fatal("Failed to generate NZB file: %v", err)
	}
	log.LogNZBCreation(filePath, nzbPath)

	log.Info("Posting completed successfully!")
	log.Info("NZB file: %s", nzbPath)
}

func uploadParts(pool *nntp.ConnectionPool, parts []*models.FilePart, postingConfig *models.PostingConfig, yencEnc *yenc.Encoder, log *logger.Logger) ([]*models.PostSegment, error) {
	var segments []*models.PostSegment
	
	for i, part := range parts {
		client, err := pool.GetClient()
		if err != nil {
			return nil, fmt.Errorf("failed to get client: %w", err)
		}

		// Join group
		if err := client.JoinGroup(postingConfig.Group); err != nil {
			return nil, fmt.Errorf("failed to join group: %w", err)
		}

		// Encode part
		encoded := yencEnc.Encode(part.Data, part.FileName, part.PartNumber, len(parts))
		
		// Create subject
		subject := fmt.Sprintf("%s [%d/%d] - %d bytes", 
			part.FileName, part.PartNumber, len(parts), part.Size)

		// Upload part
		messageID, err := client.PostArticle(
			postingConfig.Group,
			subject,
			fmt.Sprintf("%s <%s>", postingConfig.PosterName, postingConfig.PosterEmail),
			encoded,
			postingConfig.CustomHeaders,
		)
		
		if err != nil {
			return nil, fmt.Errorf("failed to post part %d: %w", part.PartNumber, err)
		}

		segment := &models.PostSegment{
			MessageID:   messageID,
			PartNumber:  part.PartNumber,
			TotalParts:  len(parts),
			FileName:    part.FileName,
			Subject:     subject,
			PostedAt:    time.Now(),
			BytesPosted: part.Size,
		}
		
		segments = append(segments, segment)
		log.LogUploadProgress(part.FileName, part.PartNumber, len(parts), part.Size)
	}

	return segments, nil
}

func sumPartSizes(parts []*models.FilePart) int64 {
	var total int64
	for _, part := range parts {
		total += part.Size
	}
	return total
}