package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ypost/internal/logger"
	"ypost/internal/nzb"
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

type recordingPostState struct {
	mu              sync.Mutex
	active          int
	maxActive       int
	activeByPoster  map[int]int
	maxByPoster     map[int]int
	completionOrder []int
}

type recordingPoster struct {
	id    int
	state *recordingPostState
}

func (p *recordingPoster) PostArticleContext(ctx context.Context, _, subject, _, _ string, _ map[string]string) (string, error) {
	part, err := strconv.Atoi(subject)
	if err != nil {
		return "", err
	}
	p.state.mu.Lock()
	p.state.active++
	p.state.activeByPoster[p.id]++
	if p.state.active > p.state.maxActive {
		p.state.maxActive = p.state.active
	}
	if p.state.activeByPoster[p.id] > p.state.maxByPoster[p.id] {
		p.state.maxByPoster[p.id] = p.state.activeByPoster[p.id]
	}
	p.state.mu.Unlock()

	delay := time.Duration(6-part) * 5 * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return "", ctx.Err()
	}

	p.state.mu.Lock()
	p.state.active--
	p.state.activeByPoster[p.id]--
	p.state.completionOrder = append(p.state.completionOrder, part)
	p.state.mu.Unlock()
	return fmt.Sprintf("<part-%d>", part), nil
}

func TestPostNyuuFileStreamsConcurrentlyAndWritesOrderedNZB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release.001")
	data := []byte(strings.Repeat("article-data-", 14))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	cfg := validTestConfig()
	cfg.Posting.MaxArticleSize = 32
	cfg.Posting.SubjectTemplate = "{{.Index}}"
	state := &recordingPostState{
		activeByPoster: make(map[int]int),
		maxByPoster:    make(map[int]int),
	}
	posters := make([]articlePoster, 3)
	for i := range posters {
		posters[i] = &recordingPoster{id: i, state: state}
	}
	nzbGen, err := nzb.NewNyuuGenerator(filepath.Join(dir, "release.nzb"), "poster")
	if err != nil {
		t.Fatal(err)
	}
	log, err := logger.New(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	var uploaded atomic.Int64
	if err := postNyuuFileWithPosters(posters, path, cfg, nzbGen, log, func(delta int64) {
		uploaded.Add(delta)
	}); err != nil {
		t.Fatal(err)
	}
	if err := nzbGen.Close(); err != nil {
		t.Fatal(err)
	}
	if uploaded.Load() != int64(len(data)) {
		t.Fatalf("uploaded bytes = %d, want %d", uploaded.Load(), len(data))
	}
	if state.maxActive < 2 {
		t.Fatalf("maximum concurrent posts = %d, want at least 2", state.maxActive)
	}
	if len(state.completionOrder) == 0 || state.completionOrder[0] == 1 {
		t.Fatalf("completion order = %v, test did not exercise out-of-order results", state.completionOrder)
	}
	for id, maximum := range state.maxByPoster {
		if maximum != 1 {
			t.Fatalf("poster %d handled %d concurrent posts, want 1", id, maximum)
		}
	}

	content, err := os.ReadFile(nzbGen.GetPath())
	if err != nil {
		t.Fatal(err)
	}
	previous := -1
	totalParts := (len(data) + 31) / 32
	for part := 1; part <= totalParts; part++ {
		position := strings.Index(string(content), fmt.Sprintf(">part-%d</segment>", part))
		if position < 0 {
			t.Fatalf("NZB is missing part %d", part)
		}
		if position <= previous {
			t.Fatalf("NZB part %d is out of order", part)
		}
		previous = position
	}
}

type cancellingPostState struct {
	started   atomic.Int64
	cancelled atomic.Int64
	ready     chan struct{}
	once      sync.Once
}

type cancellingPoster struct {
	state *cancellingPostState
}

func (p *cancellingPoster) PostArticleContext(ctx context.Context, _, subject, _, _ string, _ map[string]string) (string, error) {
	part, err := strconv.Atoi(subject)
	if err != nil {
		return "", err
	}
	if p.state.started.Add(1) == 3 {
		p.state.once.Do(func() { close(p.state.ready) })
	}
	select {
	case <-p.state.ready:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if part == 1 {
		return "", errors.New("synthetic posting failure")
	}
	<-ctx.Done()
	p.state.cancelled.Add(1)
	return "", ctx.Err()
}

func TestPostNyuuFileCancelsWorkersAfterFirstFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release.001")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 160)), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := validTestConfig()
	cfg.Posting.MaxArticleSize = 16
	cfg.Posting.SubjectTemplate = "{{.Index}}"
	state := &cancellingPostState{ready: make(chan struct{})}
	posters := []articlePoster{
		&cancellingPoster{state: state},
		&cancellingPoster{state: state},
		&cancellingPoster{state: state},
	}
	nzbGen, err := nzb.NewNyuuGenerator(filepath.Join(dir, "release.nzb"), "poster")
	if err != nil {
		t.Fatal(err)
	}
	defer nzbGen.Close()
	log, err := logger.New(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	err = postNyuuFileWithPosters(posters, path, cfg, nzbGen, log, nil)
	if err == nil || !strings.Contains(err.Error(), "synthetic posting failure") {
		t.Fatalf("post error = %v, want synthetic posting failure", err)
	}
	if got := state.started.Load(); got != 3 {
		t.Fatalf("started articles = %d, want only the 3 in-flight workers", got)
	}
	if got := state.cancelled.Load(); got != 2 {
		t.Fatalf("cancelled workers = %d, want 2", got)
	}
}
