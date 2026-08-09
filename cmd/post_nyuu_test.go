package cmd

import (
	"reflect"
	"testing"
)

func TestPostingOrderKeepsOneReleaseInRecommendedOrder(t *testing.T) {
	got := postingOrder("release.sfv", []string{"release.par2", "release.vol00+01.par2"}, []string{"release.001", "release.002"})
	want := []string{"release.par2", "release.sfv", "release.001", "release.002", "release.vol00+01.par2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("postingOrder() = %v, want %v", got, want)
	}
}

func TestRenderSubject(t *testing.T) {
	got := renderSubject(`{{.Filename}} yEnc ({{.ChunkIndex}}/{{.TotalChunks}})`, "release.001", 2, 7, 42)
	if got != "release.001 yEnc (2/7)" {
		t.Fatalf("renderSubject() = %q", got)
	}
}

func TestValidatePostOptions(t *testing.T) {
	cfg := validTestConfig()
	cfg.Par2.Redundancy = 101
	if err := validatePostOptions(cfg); err == nil {
		t.Fatal("expected invalid redundancy error")
	}
}
