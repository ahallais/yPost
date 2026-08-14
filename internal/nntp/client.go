package nntp

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"ypost/pkg/models"
)

const (
	uploadWriteChunkSize  = 192 * 1024
	uploadWriteBufferSize = uploadWriteChunkSize + 4096
)

// Client represents an NNTP client connection
type Client struct {
	conn         net.Conn
	reader       *textproto.Reader
	writer       *textproto.Writer
	config       *models.ServerConfig
	connected    bool
	mu           sync.Mutex
	postMu       sync.Mutex
	reconnect    func() error
	retryHook    func(RetryEvent)
	sessionCache tls.ClientSessionCache
}

// RetryEvent describes a bounded retry before it happens.
type RetryEvent struct {
	Kind    string
	Attempt int
	Maximum int
	Delay   time.Duration
	Err     error
}

// BodyWriter writes a complete article body and must be safe to call again
// when the request is retried.
type BodyWriter func(io.Writer) error

// NewClient creates a new NNTP client
func NewClient(config *models.ServerConfig) *Client {
	return &Client{
		config: config,
	}
}

// SetRetryHook installs an optional observer used for retry diagnostics.
func (c *Client) SetRetryHook(hook func(RetryEvent)) {
	c.retryHook = hook
}

// Connect establishes connection to the NNTP server
func (c *Client) Connect() error {
	return c.ConnectContext(context.Background())
}

// ConnectContext establishes a connection and lets cancellation interrupt the
// dial, TLS handshake, and welcome-message read.
func (c *Client) ConnectContext(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	address := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	dialer := &net.Dialer{Timeout: c.config.ConnectTimeout}
	rawConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}
	var conn net.Conn = rawConn
	if c.config.SSL {
		tlsConn := tls.Client(rawConn, &tls.Config{
			ServerName:         c.config.Host,
			ClientSessionCache: c.sessionCache,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return fmt.Errorf("failed to connect to %s: %w", address, err)
		}
		conn = tlsConn
	}

	c.conn = conn
	c.reader = textproto.NewReader(bufio.NewReader(conn))
	c.writer = textproto.NewWriter(bufio.NewWriterSize(conn, uploadWriteBufferSize))
	stopDeadline := c.setCommandDeadline(ctx, conn)

	// Read welcome message
	_, _, err = c.reader.ReadCodeLine(200)
	stopDeadline()
	if err != nil {
		_ = c.conn.Close()
		if ctx.Err() != nil {
			return fmt.Errorf("failed to read welcome message: %w", ctx.Err())
		}
		return fmt.Errorf("failed to read welcome message: %w", err)
	}

	c.connected = true
	return nil
}

// Authenticate performs authentication with the server
func (c *Client) Authenticate() error {
	return c.AuthenticateContext(context.Background())
}

// AuthenticateContext authenticates and interrupts command I/O when ctx is
// cancelled.
func (c *Client) AuthenticateContext(ctx context.Context) error {
	if c.config.Username == "" || c.config.Password == "" {
		return nil // No authentication required
	}
	stopDeadline := c.setCommandDeadline(ctx, c.conn)
	defer stopDeadline()

	// Send AUTHINFO USER
	err := c.writer.PrintfLine("AUTHINFO USER %s", c.config.Username)
	if err != nil {
		return fmt.Errorf("failed to send username: %w", err)
	}

	_, _, err = c.reader.ReadCodeLine(381)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Send AUTHINFO PASS
	err = c.writer.PrintfLine("AUTHINFO PASS %s", c.config.Password)
	if err != nil {
		return fmt.Errorf("failed to send password: %w", err)
	}

	_, _, err = c.reader.ReadCodeLine(281)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	return nil
}

func (c *Client) setCommandDeadline(ctx context.Context, conn net.Conn) func() {
	if c.config.CommandTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.config.CommandTimeout))
	}
	deadlineSet := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
		close(deadlineSet)
	})
	return func() {
		if !stop() {
			<-deadlineSet
		}
		_ = conn.SetDeadline(time.Time{})
	}
}

// PostArticle posts an article to the specified newsgroup
func (c *Client) PostArticle(group string, subject string, from string, body string, headers map[string]string) (string, error) {
	return c.PostArticleContext(context.Background(), group, subject, from, body, headers)
}

// PostArticleContext posts an article and interrupts socket I/O when ctx is
// cancelled. Transport failures and code 441 responses use their separately
// configured retry limits while preserving the same Message-ID.
func (c *Client) PostArticleContext(ctx context.Context, group string, subject string, from string, body string, headers map[string]string) (messageID string, err error) {
	return c.PostArticleStreamContext(ctx, group, subject, from, func(writer io.Writer) error {
		_, err := io.WriteString(writer, body)
		return err
	}, headers)
}

// PostArticleStreamContext posts a replayable streaming body. It retains only
// the caller's raw article buffer and the bounded network buffer.
func (c *Client) PostArticleStreamContext(ctx context.Context, group string, subject string, from string, writeBody BodyWriter, headers map[string]string) (messageID string, err error) {
	c.postMu.Lock()
	defer c.postMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}

	messageID = c.generateMessageID()
	transportRetries := 0
	postRetries := 0
	for {
		responseID, postErr := c.postArticleOnce(ctx, group, subject, from, writeBody, headers, messageID)
		err = postErr
		if err == nil {
			if responseID != "" {
				messageID = responseID
			}
			return messageID, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if isPostRejection(err) && postRetries < c.postRetryLimit() {
			postRetries++
			c.emitRetry(RetryEvent{Kind: "post rejection", Attempt: postRetries, Maximum: c.postRetryLimit(), Err: err})
			continue
		}
		if !isTransportError(err) {
			return "", err
		}
		if transportRetries >= c.requestRetryLimit() {
			if transportRetries == 0 {
				return "", err
			}
			return "", fmt.Errorf("posting failed after %d reconnect attempts: %w", transportRetries, err)
		}

		for {
			transportRetries++
			c.emitRetry(RetryEvent{Kind: "connection failure", Attempt: transportRetries, Maximum: c.requestRetryLimit(), Delay: c.reconnectDelay(), Err: err})
			if err := waitForContext(ctx, c.reconnectDelay()); err != nil {
				return "", err
			}
			reconnectErr := c.reconnectForPost()
			if reconnectErr == nil {
				break
			}
			err = reconnectErr
			if transportRetries >= c.requestRetryLimit() {
				return "", fmt.Errorf("reconnect after posting failure: %w", reconnectErr)
			}
		}
	}
}

func (c *Client) postArticleOnce(ctx context.Context, group string, subject string, from string, writeBody BodyWriter, headers map[string]string, messageID string) (responseMessageID string, err error) {
	c.mu.Lock()
	connected := c.connected
	conn := c.conn
	c.mu.Unlock()
	if !connected {
		return "", fmt.Errorf("not connected to server")
	}
	if c.config != nil && c.config.CommandTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.config.CommandTimeout))
	}

	watchDone := make(chan struct{})
	stopWatch := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			// net.Conn permits deadlines to be changed concurrently with I/O.
			_ = conn.SetDeadline(time.Now())
		case <-stopWatch:
		}
	}()
	defer func() {
		close(stopWatch)
		<-watchDone
		if ctx.Err() != nil {
			c.mu.Lock()
			_ = conn.Close()
			c.connected = false
			c.mu.Unlock()
		} else {
			_ = conn.SetDeadline(time.Time{})
		}
		if err != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
	}()

	// Send POST command
	err = c.writer.PrintfLine("POST")
	if err != nil {
		return "", fmt.Errorf("failed to send POST command: %w", err)
	}

	_, _, err = c.reader.ReadCodeLine(340)
	if err != nil {
		return "", fmt.Errorf("server rejected POST command: %w", err)
	}

	// Write headers
	headersToSend := map[string]string{
		"From":         from,
		"Subject":      subject,
		"Newsgroups":   group,
		"Message-ID":   messageID,
		"Date":         time.Now().Format(time.RFC1123Z),
		"Content-Type": "text/plain; charset=UTF-8",
	}

	// Add custom headers
	for k, v := range headers {
		headersToSend[k] = v
	}

	// Send headers
	for key, value := range headersToSend {
		err := c.writer.PrintfLine("%s: %s", key, value)
		if err != nil {
			return "", fmt.Errorf("failed to send header %s: %w", key, err)
		}
	}

	// Send empty line to separate headers from body
	err = c.writer.PrintfLine("")
	if err != nil {
		return "", fmt.Errorf("failed to send header separator: %w", err)
	}

	// Send the body through a large buffered writer. PrintfLine flushes after
	// every line, which turns a yEnc article into thousands of tiny TLS writes.
	prepareFlush := func() {
		if c.config != nil && c.config.CommandTimeout > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(c.config.CommandTimeout))
		}
	}
	wireWriter := newArticleWireWriter(c.writer.W, prepareFlush)
	if err = writeBody(wireWriter); err != nil {
		return "", fmt.Errorf("failed to stream article body: %w", err)
	}
	if err = wireWriter.Close(); err != nil {
		return "", fmt.Errorf("failed to send article body: %w", err)
	}

	if c.config != nil && c.config.PostTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(c.config.PostTimeout))
	}
	_, response, responseErr := c.reader.ReadCodeLine(240)
	err = responseErr
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("server rejected article: %w", err)
	}

	return responseMessageIDFromText(response), nil
}

func (c *Client) generateMessageID() string {
	// Match the Node.js format: random chars + '-' + timestamp + '@nyuu'.
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	timestamp := fmt.Sprintf("%013d", time.Now().UnixNano()/1000000)
	var randomChars strings.Builder
	for i := 0; i < 24; i++ {
		randomChars.WriteByte(chars[time.Now().UnixNano()%int64(len(chars))])
	}
	return fmt.Sprintf("<%s-%s@nyuu>", randomChars.String()[:24], timestamp)
}

func isTransportError(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)
}

func isPostRejection(err error) bool {
	var protocolError *textproto.Error
	return errors.As(err, &protocolError) && protocolError.Code == 441
}

func responseMessageIDFromText(response string) string {
	start := strings.IndexByte(response, '<')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(response[start+1:], '>')
	if end < 1 {
		return ""
	}
	value := response[start+1 : start+1+end]
	if strings.ContainsAny(value, "<>\r\n\t ") {
		return ""
	}
	return "<" + value + ">"
}

func (c *Client) emitRetry(event RetryEvent) {
	if c.retryHook != nil {
		c.retryHook(event)
	}
}

func (c *Client) requestRetryLimit() int {
	if c.config == nil {
		return 1
	}
	return c.config.RequestRetries
}

func (c *Client) postRetryLimit() int {
	if c.config == nil {
		return 0
	}
	return c.config.PostRetries
}

func (c *Client) reconnectDelay() time.Duration {
	if c.config == nil {
		return 0
	}
	return c.config.ReconnectDelay
}

func waitForContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type articleWireWriter struct {
	writer       *bufio.Writer
	prepareFlush func()
	atLineStart  bool
	pendingCR    bool
	wrote        bool
	closed       bool
}

func newArticleWireWriter(writer *bufio.Writer, prepareFlush func()) *articleWireWriter {
	return &articleWireWriter{writer: writer, prepareFlush: prepareFlush, atLineStart: true}
}

func (w *articleWireWriter) Write(data []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("article body writer is closed")
	}
	consumed := 0
	for consumed < len(data) {
		value := data[consumed]
		if w.pendingCR {
			if _, err := w.writer.WriteString("\r\n"); err != nil {
				return consumed, err
			}
			w.pendingCR = false
			w.atLineStart = true
			if value == '\n' {
				w.wrote = true
				consumed++
				if err := w.flush(false); err != nil {
					return consumed, err
				}
				continue
			}
		}
		if w.atLineStart && value == '.' {
			if err := w.writer.WriteByte('.'); err != nil {
				return consumed, err
			}
		}
		switch value {
		case '\r':
			w.pendingCR = true
		case '\n':
			if _, err := w.writer.WriteString("\r\n"); err != nil {
				return consumed, err
			}
			w.atLineStart = true
		default:
			if err := w.writer.WriteByte(value); err != nil {
				return consumed, err
			}
			w.atLineStart = false
		}
		w.wrote = true
		consumed++
		if err := w.flush(false); err != nil {
			return consumed, err
		}
	}
	return consumed, nil
}

func (w *articleWireWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.pendingCR {
		if _, err := w.writer.WriteString("\r\n"); err != nil {
			return err
		}
		w.pendingCR = false
		w.atLineStart = true
	}
	if !w.wrote || !w.atLineStart {
		if _, err := w.writer.WriteString("\r\n"); err != nil {
			return err
		}
	}
	if _, err := w.writer.WriteString(".\r\n"); err != nil {
		return err
	}
	return w.flush(true)
}

func (w *articleWireWriter) flush(force bool) error {
	if !force && w.writer.Buffered() < uploadWriteChunkSize {
		return nil
	}
	if w.prepareFlush != nil {
		w.prepareFlush()
	}
	return w.writer.Flush()
}

func writeArticleBody(writer *bufio.Writer, body string, prepareFlush func()) error {
	wireWriter := newArticleWireWriter(writer, prepareFlush)
	if _, err := io.WriteString(wireWriter, body); err != nil {
		return err
	}
	return wireWriter.Close()
}

func (c *Client) reconnectForPost() error {
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.connected = false
	c.mu.Unlock()

	if c.reconnect != nil {
		return c.reconnect()
	}
	if err := c.Connect(); err != nil {
		return err
	}
	if err := c.Authenticate(); err != nil {
		_ = c.Quit()
		return err
	}
	return nil
}

// JoinGroup joins the specified newsgroup
func (c *Client) JoinGroup(group string) error {
	err := c.writer.PrintfLine("GROUP %s", group)
	if err != nil {
		return fmt.Errorf("failed to send GROUP command: %w", err)
	}

	_, _, err = c.reader.ReadCodeLine(211)
	if err != nil {
		return fmt.Errorf("failed to join group %s: %w", group, err)
	}

	return nil
}

// Quit closes the connection
func (c *Client) Quit() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	_ = c.writer.PrintfLine("QUIT")
	c.conn.Close()
	c.connected = false

	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// ConnectionPool manages multiple NNTP connections
type ConnectionPool struct {
	clients      []*Client
	config       *models.ServerConfig
	maxConns     int
	current      int
	newClient    func(*models.ServerConfig) *Client
	sessionCache tls.ClientSessionCache
	mu           sync.Mutex
}

// ConnectionResult reports one serially established connection or the error
// that stopped connection creation.
type ConnectionResult struct {
	Client *Client
	Err    error
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(config *models.ServerConfig, maxConns int) *ConnectionPool {
	return &ConnectionPool{
		config:       config,
		maxConns:     maxConns,
		clients:      make([]*Client, 0, maxConns),
		newClient:    NewClient,
		sessionCache: tls.NewLRUClientSessionCache(maxConns),
	}
}

// ConnectAll creates and authenticates the configured number of clients.
// The returned slice is stable and lets callers dedicate one client to each
// worker instead of sharing busy connections through round-robin selection.
func (p *ConnectionPool) ConnectAll() ([]*Client, error) {
	return p.ConnectAllWithProgress(nil)
}

// ConnectAllWithProgress creates and authenticates all clients and reports
// each completed connection. Connections are intentionally opened
// sequentially to keep TLS handshakes inexpensive on small NAS systems.
func (p *ConnectionPool) ConnectAllWithProgress(progress func(completed, total int)) ([]*Client, error) {
	for result := range p.ConnectSequentially(context.Background(), progress) {
		if result.Err != nil {
			return nil, result.Err
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	clients := make([]*Client, len(p.clients))
	copy(clients, p.clients)
	return clients, nil
}

// ConnectSequentially opens and authenticates connections one at a time, but
// publishes each client immediately so callers can begin useful work while
// later TLS handshakes are still in progress. Cancelling ctx stops connection
// creation and interrupts an active dial, handshake, or authentication.
func (p *ConnectionPool) ConnectSequentially(ctx context.Context, progress func(completed, total int)) <-chan ConnectionResult {
	results := make(chan ConnectionResult, p.maxConns)
	go func() {
		defer close(results)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			p.mu.Lock()
			if len(p.clients) >= p.maxConns {
				p.mu.Unlock()
				return
			}
			client := p.newClient(p.config)
			client.sessionCache = p.sessionCache
			p.mu.Unlock()

			if err := client.ConnectContext(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				results <- ConnectionResult{Err: err}
				return
			}
			if err := client.AuthenticateContext(ctx); err != nil {
				_ = client.Quit()
				if ctx.Err() != nil {
					return
				}
				results <- ConnectionResult{Err: err}
				return
			}

			p.mu.Lock()
			p.clients = append(p.clients, client)
			completed := len(p.clients)
			p.mu.Unlock()
			if progress != nil {
				progress(completed, p.maxConns)
			}
			results <- ConnectionResult{Client: client}
		}
	}()
	return results
}

// GetClient returns an available client from the pool
func (p *ConnectionPool) GetClient() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create new client if we haven't reached max connections
	if len(p.clients) < p.maxConns {
		client := p.newClient(p.config)
		client.sessionCache = p.sessionCache
		err := client.Connect()
		if err != nil {
			return nil, err
		}

		err = client.Authenticate()
		if err != nil {
			client.Quit()
			return nil, err
		}

		p.clients = append(p.clients, client)
		return client, nil
	}

	// Reuse existing client (round-robin)
	if len(p.clients) > 0 {
		client := p.clients[p.current%len(p.clients)]
		p.current++
		return client, nil
	}

	return nil, fmt.Errorf("no clients available")
}

// CloseAll closes all connections in the pool
func (p *ConnectionPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, client := range p.clients {
		client.Quit()
	}
	p.clients = nil
}
