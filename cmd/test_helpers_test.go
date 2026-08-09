package cmd

import "ypost/pkg/models"

func validTestConfig() *models.Config {
	cfg := &models.Config{}
	cfg.Posting.Group = "alt.binaries.test"
	cfg.Posting.MaxPartSize = 768000
	cfg.Posting.MaxArticleSize = 768000
	cfg.Posting.MaxLineLength = 128
	cfg.Par2.Redundancy = 10
	return cfg
}
