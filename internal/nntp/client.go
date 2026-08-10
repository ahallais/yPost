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
	conn      net.Conn
	reader    *textproto.Reader
	writer    *textproto.Writer
	config    *models.ServerConfig
	connected bool
	mu        sync.Mutex
	postMu    sync.Mutex
	reconnect func() error
}

// NewClient creates a new NNTP client
func NewClient(config *models.ServerConfig) *Client {
	return &Client{
		config: config,
	}
}

// Connect establishes connection to the NNTP server
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	address := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	var conn net.Conn
	var err error

	dialer := &net.Dialer{Timeout: c.config.ConnectTimeout}
	if c.config.SSL {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{
			ServerName: c.config.Host,
		})
	} else {
		conn, err = dialer.Dial("tcp", address)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}

	c.conn = conn
	c.reader = textproto.NewReader(bufio.NewReader(conn))
	c.writer = textproto.NewWriter(bufio.NewWriterSize(conn, uploadWriteBufferSize))
	if c.config.CommandTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.config.CommandTimeout))
	}

	// Read welcome message
	_, _, err = c.reader.ReadCodeLine(200)
	if err != nil {
		c.conn.Close()
		return fmt.Errorf("failed to read welcome message: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})

	c.connected = true
	return nil
}

// Authenticate performs authentication with the server
func (c *Client) Authenticate() error {
	if c.config.Username == "" || c.config.Password == "" {
		return nil // No authentication required
	}
	if c.config.CommandTimeout > 0 {
		_ = c.conn.SetDeadline(time.Now().Add(c.config.CommandTimeout))
		defer c.conn.SetDeadline(time.Time{})
	}

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

// PostArticle posts an article to the specified newsgroup
func (c *Client) PostArticle(group string, subject string, from string, body string, headers map[string]string) (string, error) {
	return c.PostArticleContext(context.Background(), group, subject, from, body, headers)
}

// PostArticleContext posts an article and interrupts socket I/O when ctx is
// cancelled. A transport failure is retried once on a fresh connection using
// the same Message-ID; explicit NNTP rejection responses are not retried.
func (c *Client) PostArticleContext(ctx context.Context, group string, subject string, from string, body string, headers map[string]string) (messageID string, err error) {
	c.postMu.Lock()
	defer c.postMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}

	messageID = c.generateMessageID()
	transportRetries := 0
	postRetries := 0
	for {
		err = c.postArticleOnce(ctx, group, subject, from, body, headers, messageID)
		if err == nil {
			return messageID, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if isPostRejection(err) && postRetries < c.postRetryLimit() {
			postRetries++
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
			if err := waitForContext(ctx, c.reconnectDelay()); err != nil {
				return "", err
			}
			reconnectErr := c.reconnectForPost()
			if reconnectErr == nil {
				break
			}
			if transportRetries >= c.requestRetryLimit() {
				return "", fmt.Errorf("reconnect after posting failure: %w", reconnectErr)
			}
		}
	}
}

func (c *Client) postArticleOnce(ctx context.Context, group string, subject string, from string, body string, headers map[string]string, messageID string) (err error) {
	c.mu.Lock()
	connected := c.connected
	conn := c.conn
	c.mu.Unlock()
	if !connected {
		return fmt.Errorf("not connected to server")
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
		return fmt.Errorf("failed to send POST command: %w", err)
	}

	_, _, err = c.reader.ReadCodeLine(340)
	if err != nil {
		return fmt.Errorf("server rejected POST command: %w", err)
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
			return fmt.Errorf("failed to send header %s: %w", key, err)
		}
	}

	// Send empty line to separate headers from body
	err = c.writer.PrintfLine("")
	if err != nil {
		return fmt.Errorf("failed to send header separator: %w", err)
	}

	// Send the body through a large buffered writer. PrintfLine flushes after
	// every line, which turns a yEnc article into thousands of tiny TLS writes.
	prepareFlush := func() {
		if c.config != nil && c.config.CommandTimeout > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(c.config.CommandTimeout))
		}
	}
	if err = writeArticleBody(c.writer.W, body, prepareFlush); err != nil {
		return fmt.Errorf("failed to send article body: %w", err)
	}

	if c.config != nil && c.config.PostTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(c.config.PostTimeout))
	}
	_, _, err = c.reader.ReadCodeLine(240)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("server rejected article: %w", err)
	}

	return nil
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

func writeArticleBody(writer *bufio.Writer, body string, prepareFlush func()) error {
	flushChunk := func(force bool) error {
		if !force && writer.Buffered() < uploadWriteChunkSize {
			return nil
		}
		if prepareFlush != nil {
			prepareFlush()
		}
		return writer.Flush()
	}
	atLineStart := true
	for i := 0; i < len(body); i++ {
		value := body[i]
		if atLineStart && value == '.' {
			if err := writer.WriteByte('.'); err != nil {
				return err
			}
		}
		switch value {
		case '\r':
			if i+1 < len(body) && body[i+1] == '\n' {
				i++
			}
			if _, err := writer.WriteString("\r\n"); err != nil {
				return err
			}
			atLineStart = true
		case '\n':
			if _, err := writer.WriteString("\r\n"); err != nil {
				return err
			}
			atLineStart = true
		default:
			if err := writer.WriteByte(value); err != nil {
				return err
			}
			atLineStart = false
		}
		if err := flushChunk(false); err != nil {
			return err
		}
	}
	if len(body) == 0 || !atLineStart {
		if _, err := writer.WriteString("\r\n"); err != nil {
			return err
		}
	}
	if _, err := writer.WriteString(".\r\n"); err != nil {
		return err
	}
	return flushChunk(true)
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
	clients   []*Client
	config    *models.ServerConfig
	maxConns  int
	current   int
	newClient func(*models.ServerConfig) *Client
	mu        sync.Mutex
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(config *models.ServerConfig, maxConns int) *ConnectionPool {
	return &ConnectionPool{
		config:    config,
		maxConns:  maxConns,
		clients:   make([]*Client, 0, maxConns),
		newClient: NewClient,
	}
}

// ConnectAll creates and authenticates the configured number of clients.
// The returned slice is stable and lets callers dedicate one client to each
// worker instead of sharing busy connections through round-robin selection.
func (p *ConnectionPool) ConnectAll() ([]*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for len(p.clients) < p.maxConns {
		client := p.newClient(p.config)
		if err := client.Connect(); err != nil {
			return nil, err
		}
		if err := client.Authenticate(); err != nil {
			_ = client.Quit()
			return nil, err
		}
		p.clients = append(p.clients, client)
	}

	clients := make([]*Client, len(p.clients))
	copy(clients, p.clients)
	return clients, nil
}

// GetClient returns an available client from the pool
func (p *ConnectionPool) GetClient() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create new client if we haven't reached max connections
	if len(p.clients) < p.maxConns {
		client := p.newClient(p.config)
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
