package nntp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"ypost/pkg/models"
)

func TestPostArticlePreservesBodyBytesAndCRLF(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := &Client{
		conn:      clientConn,
		reader:    textproto.NewReader(bufio.NewReader(clientConn)),
		writer:    textproto.NewWriter(bufio.NewWriter(clientConn)),
		connected: true,
	}

	received := make(chan []string, 1)
	go func() {
		r := bufio.NewReader(serverConn)
		line, _ := r.ReadString('\n')
		if line != "POST\r\n" {
			received <- []string{"unexpected command: " + line}
			return
		}
		_, _ = serverConn.Write([]byte("340 send article\r\n"))

		var lines []string
		for {
			line, _ = r.ReadString('\n')
			lines = append(lines, line)
			if line == ".\r\n" {
				break
			}
		}
		received <- lines
		_, _ = serverConn.Write([]byte("240 article received\r\n"))
	}()

	_, err := c.PostArticle("alt.test", "subject", "poster", "abc%def\r\n.second\r\n", nil)
	if err != nil {
		t.Fatal(err)
	}

	lines := <-received
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, "\r\n\r\nabc%def\r\n..second\r\n.\r\n") {
		t.Fatalf("article body was changed on the wire:\n%q", joined)
	}
	if strings.Contains(joined, "\r\r\n") {
		t.Fatal("article contains doubled carriage returns")
	}
	if strings.Contains(joined, "%!") {
		t.Fatal("percent bytes were interpreted as formatting directives")
	}
}

func TestPostArticleContextInterruptsBlockedIO(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	c := &Client{
		conn:      clientConn,
		reader:    textproto.NewReader(bufio.NewReader(clientConn)),
		writer:    textproto.NewWriter(bufio.NewWriter(clientConn)),
		connected: true,
	}

	articleReceived := make(chan struct{})
	go func() {
		r := bufio.NewReader(serverConn)
		_, _ = r.ReadString('\n')
		_, _ = serverConn.Write([]byte("340 send article\r\n"))
		for {
			line, _ := r.ReadString('\n')
			if line == ".\r\n" {
				close(articleReceived)
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.PostArticleContext(ctx, "alt.test", "subject", "poster", "body\r\n", nil)
		done <- err
	}()
	select {
	case <-articleReceived:
	case <-time.After(time.Second):
		t.Fatal("article was not sent")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("post error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked NNTP read was not interrupted")
	}
	if c.IsConnected() {
		t.Fatal("cancelled NNTP connection should not be reusable")
	}
}

func TestConnectionPoolConnectAllCreatesConfiguredClients(t *testing.T) {
	pool := NewConnectionPool(&models.ServerConfig{}, 3)
	created := 0
	pool.newClient = func(config *models.ServerConfig) *Client {
		created++
		return &Client{config: config, connected: true}
	}
	clients, err := pool.ConnectAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 3 {
		t.Fatalf("connected clients = %d, want 3", len(clients))
	}
	if created != 3 {
		t.Fatalf("created clients = %d, want 3", created)
	}
}

func TestConnectionPoolConnectAllReportsProgress(t *testing.T) {
	pool := NewConnectionPool(&models.ServerConfig{}, 3)
	pool.newClient = func(config *models.ServerConfig) *Client {
		return &Client{config: config, connected: true}
	}

	var completed []int
	clients, err := pool.ConnectAllWithProgress(func(current, total int) {
		if total != 3 {
			t.Fatalf("progress total = %d, want 3", total)
		}
		completed = append(completed, current)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 3 {
		t.Fatalf("connected clients = %d, want 3", len(clients))
	}
	if len(completed) != 3 || completed[0] != 1 || completed[1] != 2 || completed[2] != 3 {
		t.Fatalf("progress = %v, want [1 2 3]", completed)
	}
}

type servedArticle struct {
	messageID string
	err       error
}

func serveArticle(conn net.Conn, response string) servedArticle {
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return servedArticle{err: err}
	}
	if line != "POST\r\n" {
		return servedArticle{err: fmt.Errorf("unexpected command %q", line)}
	}
	if _, err := conn.Write([]byte("340 send article\r\n")); err != nil {
		return servedArticle{err: err}
	}

	var messageID string
	inBody := false
	for {
		line, err = reader.ReadString('\n')
		if err != nil {
			return servedArticle{err: err}
		}
		if !inBody {
			if strings.HasPrefix(line, "Message-ID: ") {
				messageID = strings.TrimSpace(strings.TrimPrefix(line, "Message-ID: "))
			}
			if line == "\r\n" {
				inBody = true
			}
			continue
		}
		if line == ".\r\n" {
			break
		}
	}
	if response != "" {
		if _, err := conn.Write([]byte(response)); err != nil {
			return servedArticle{err: err}
		}
	}
	return servedArticle{messageID: messageID}
}

func TestPostArticleContextReconnectsOnceWithSameMessageID(t *testing.T) {
	firstClient, firstServer := net.Pipe()
	secondClient, secondServer := net.Pipe()
	defer firstClient.Close()
	defer firstServer.Close()
	defer secondClient.Close()
	defer secondServer.Close()
	c := &Client{
		conn:      firstClient,
		reader:    textproto.NewReader(bufio.NewReader(firstClient)),
		writer:    textproto.NewWriter(bufio.NewWriter(firstClient)),
		connected: true,
	}
	var reconnects int
	var retryEvents []RetryEvent
	c.SetRetryHook(func(event RetryEvent) {
		retryEvents = append(retryEvents, event)
	})
	c.reconnect = func() error {
		reconnects++
		c.mu.Lock()
		c.conn = secondClient
		c.reader = textproto.NewReader(bufio.NewReader(secondClient))
		c.writer = textproto.NewWriter(bufio.NewWriter(secondClient))
		c.connected = true
		c.mu.Unlock()
		return nil
	}

	firstResult := make(chan servedArticle, 1)
	go func() {
		result := serveArticle(firstServer, "")
		firstResult <- result
		_ = firstServer.Close()
	}()
	secondResult := make(chan servedArticle, 1)
	go func() {
		secondResult <- serveArticle(secondServer, "240 article received\r\n")
	}()

	messageID, err := c.PostArticleContext(context.Background(), "alt.test", "subject", "poster", "body\r\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := <-firstResult
	second := <-secondResult
	if first.err != nil {
		t.Fatal(first.err)
	}
	if second.err != nil {
		t.Fatal(second.err)
	}
	if reconnects != 1 {
		t.Fatalf("reconnects = %d, want 1", reconnects)
	}
	if len(retryEvents) != 1 || retryEvents[0].Kind != "connection failure" || retryEvents[0].Attempt != 1 {
		t.Fatalf("retry events = %#v", retryEvents)
	}
	if first.messageID != messageID || second.messageID != messageID {
		t.Fatalf("message IDs differ: first=%q second=%q returned=%q", first.messageID, second.messageID, messageID)
	}
}

func TestPostArticleContextDoesNotRetryProtocolRejection(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	c := &Client{
		conn:      clientConn,
		reader:    textproto.NewReader(bufio.NewReader(clientConn)),
		writer:    textproto.NewWriter(bufio.NewWriter(clientConn)),
		connected: true,
	}
	var reconnects int
	c.reconnect = func() error {
		reconnects++
		return nil
	}
	result := make(chan servedArticle, 1)
	go func() {
		result <- serveArticle(serverConn, "441 posting failed\r\n")
	}()

	_, err := c.PostArticleContext(context.Background(), "alt.test", "subject", "poster", "body\r\n", nil)
	if err == nil {
		t.Fatal("expected server rejection")
	}
	if served := <-result; served.err != nil {
		t.Fatal(served.err)
	}
	if reconnects != 0 {
		t.Fatalf("reconnects = %d, want 0", reconnects)
	}
}

func TestPostArticleContextRetries441Separately(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	c := &Client{
		conn:      clientConn,
		reader:    textproto.NewReader(bufio.NewReader(clientConn)),
		writer:    textproto.NewWriter(bufio.NewWriter(clientConn)),
		connected: true,
		config:    &models.ServerConfig{PostRetries: 1},
	}
	results := make(chan servedArticle, 2)
	go func() {
		results <- serveArticle(serverConn, "441 temporary posting failure\r\n")
		results <- serveArticle(serverConn, "240 article received\r\n")
	}()

	if _, err := c.PostArticleContext(context.Background(), "alt.test", "subject", "poster", "body\r\n", nil); err != nil {
		t.Fatal(err)
	}
	first := <-results
	second := <-results
	if first.err != nil {
		t.Fatal(first.err)
	}
	if second.err != nil {
		t.Fatal(second.err)
	}
	if first.messageID != second.messageID {
		t.Fatalf("441 retry changed Message-ID from %q to %q", first.messageID, second.messageID)
	}
}

func TestPostArticleContextUsesPostResponseTimeout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	c := &Client{
		conn:      clientConn,
		reader:    textproto.NewReader(bufio.NewReader(clientConn)),
		writer:    textproto.NewWriter(bufio.NewWriter(clientConn)),
		connected: true,
		config: &models.ServerConfig{
			CommandTimeout: time.Second,
			PostTimeout:    20 * time.Millisecond,
		},
	}
	articleRead := make(chan servedArticle, 1)
	go func() {
		articleRead <- serveArticle(serverConn, "")
	}()

	started := time.Now()
	_, err := c.PostArticleContext(context.Background(), "alt.test", "subject", "poster", "body\r\n", nil)
	if err == nil || !isTransportError(err) {
		t.Fatalf("post error = %v, want transport timeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("post timeout took %v", elapsed)
	}
	if served := <-articleRead; served.err != nil {
		t.Fatal(served.err)
	}
}

type countingWriter struct {
	bytes.Buffer
	writes int
}

func (w *countingWriter) Write(data []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(data)
}

func TestWriteArticleBodyUsesLargeBufferedWrites(t *testing.T) {
	underlying := &countingWriter{}
	writer := bufio.NewWriterSize(underlying, uploadWriteBufferSize)
	body := strings.Repeat(strings.Repeat("x", 126)+"\r\n", 4000)
	if err := writeArticleBody(writer, body, nil); err != nil {
		t.Fatal(err)
	}
	if underlying.writes > 4 {
		t.Fatalf("underlying writes = %d, want at most 4 large writes", underlying.writes)
	}
}

func TestArticleWireWriterHandlesSplitCRLFAndDotStuffing(t *testing.T) {
	var output bytes.Buffer
	writer := newArticleWireWriter(bufio.NewWriter(&output), nil)
	if _, err := writer.Write([]byte("first\r")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("\n.second\r")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if want := "first\r\n..second\r\n.\r\n"; output.String() != want {
		t.Fatalf("wire body = %q, want %q", output.String(), want)
	}
}

func TestPostArticleContextUsesServerReturnedMessageID(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	c := &Client{
		conn:      clientConn,
		reader:    textproto.NewReader(bufio.NewReader(clientConn)),
		writer:    textproto.NewWriter(bufio.NewWriter(clientConn)),
		connected: true,
	}
	result := make(chan servedArticle, 1)
	go func() {
		result <- serveArticle(serverConn, "240 <server-assigned@test> article received\r\n")
	}()

	messageID, err := c.PostArticleContext(context.Background(), "alt.test", "subject", "poster", "body\r\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	if served := <-result; served.err != nil {
		t.Fatal(served.err)
	}
	if messageID != "<server-assigned@test>" {
		t.Fatalf("returned Message-ID = %q", messageID)
	}
}
