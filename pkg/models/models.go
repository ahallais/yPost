package models

import (
	"time"
)

// Config represents the application configuration
type Config struct {
	NNTP struct {
		Servers []ServerConfig `mapstructure:"servers"`
	} `mapstructure:"nntp"`
	Posting struct {
		Group           string            `mapstructure:"group"`
		PosterName      string            `mapstructure:"poster_name"`
		PosterEmail     string            `mapstructure:"poster_email"`
		SubjectTemplate string            `mapstructure:"subject_template"`
		MaxLineLength   int               `mapstructure:"max_line_length"`
		MaxPartSize     int64             `mapstructure:"max_part_size"`
		CustomHeaders   map[string]string `mapstructure:"custom_headers"`
	} `mapstructure:"posting"`
	Output struct {
		OutputDir string `mapstructure:"output_dir"`
		NZBDir    string `mapstructure:"nzb_dir"`
		LogDir    string `mapstructure:"log_dir"`
	} `mapstructure:"output"`
	Features struct {
		CreatePAR2 bool `mapstructure:"create_par2"`
		CreateSFV  bool `mapstructure:"create_sfv"`
	} `mapstructure:"features"`
}

// ServerConfig represents NNTP server configuration
type ServerConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	SSL      bool   `mapstructure:"ssl"`
	MaxConns int    `mapstructure:"max_connections"`
}

// FilePart represents a split file part
type FilePart struct {
	PartNumber int
	FileName   string
	Size       int64
	Data       []byte
	Checksum   string
}

// PostSegment represents a posted Usenet segment
type PostSegment struct {
	MessageID   string
	PartNumber  int
	TotalParts  int
	FileName    string
	Subject     string
	PostedAt    time.Time
	BytesPosted int64
}

// NZBFile represents the NZB file structure
type NZBFile struct {
	XMLName   string    `xml:"nzb"`
	Meta      NZBMeta   `xml:"head"`
	Segments  []NZBSegment `xml:"file"`
}

type NZBMeta struct {
	Title string `xml:"meta"`
}

type NZBSegment struct {
	Poster    string      `xml:"poster,attr"`
	Date      int64       `xml:"date,attr"`
	Subject   string      `xml:"subject,attr"`
	Groups    []string    `xml:"groups>group"`
	Segments  []NZBPart   `xml:"segments>segment"`
}

type NZBPart struct {
	Bytes     int64  `xml:"bytes,attr"`
	Number    int    `xml:"number,attr"`
	MessageID string `xml:",chardata"`
}

// PostingResult represents the result of a posting operation
type PostingResult struct {
	FileName   string
	FileSize   int64
	TotalParts int
	MessageIDs []string
	NZBPath    string
	PostedAt   time.Time
	Duration   time.Duration
	Success    bool
	Error      error
}

// PostingHistory represents historical posting records
type PostingHistory struct {
	ID         string    `json:"id"`
	FileName   string    `json:"file_name"`
	FileSize   int64     `json:"file_size"`
	PostedAt   time.Time `json:"posted_at"`
	TotalParts int       `json:"total_parts"`
	NZBPath    string    `json:"nzb_path"`
	Success    bool      `json:"success"`
}