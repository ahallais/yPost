package nntp

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"usenet-poster/pkg/models"
)

// Client represents an NNTP client connection
type Client struct {
	conn      net.Conn
	reader    *textproto.Reader
	writer    *textproto.Writer
	config    *models.ServerConfig
	connected bool
	mu        sync.Mutex
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
	
	if c.config.SSL {
		conn, err = tls.Dial("tcp", address, &tls.Config{
			ServerName: c.config.Host,
		})
	} else {
		conn, err = net.Dial("tcp", address)
	}
	
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}

	c.conn = conn
	c.reader = textproto.NewReader(bufio.NewReader(conn))
	c.writer = textproto.NewWriter(bufio.NewWriter(conn))

	// Read welcome message
	_, _, err = c.reader.ReadCodeLine(200)
	if err != nil {
		c.conn.Close()
		return fmt.Errorf("failed to read welcome message: %w", err)
	}

	c.connected = true
	return nil
}

// Authenticate performs authentication with the server
func (c *Client) Authenticate() error {
	if c.config.Username == "" || c.config.Password == "" {
		return nil // No authentication required
	}

	// Send AUTHINFO USER
	_, _, err := c.writer.PrintfLine("AUTHINFO USER %s", c.config.Username)
	if err != nil {
		return fmt.Errorf("failed to send username: %w", err)
	}

	_, _, err = c.reader.ReadCodeLine(381)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Send AUTHINFO PASS
	_, _, err = c.writer.PrintfLine("AUTHINFO PASS %s", c.config.Password)
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
	if !c.connected {
		return "", fmt.Errorf("not connected to server")
	}

	// Send POST command
	_, _, err := c.writer.PrintfLine("POST")
	if err != nil {
		return "", fmt.Errorf("failed to send POST command: %w", err)
	}

	_, _, err = c.reader.ReadCodeLine(340)
	if err != nil {
		return "", fmt.Errorf("server rejected POST command: %w", err)
	}

	// Generate Message-ID
	messageID := fmt.Sprintf("<%d.%d@%s>", time.Now().UnixNano(), time.Now().Unix(), c.config.Host)

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
		_, err := c.writer.PrintfLine("%s: %s", key, value)
		if err != nil {
			return "", fmt.Errorf("failed to send header %s: %w", key, err)
		}
	}

	// Send empty line to separate headers from body
	_, err = c.writer.PrintfLine("")
	if err != nil {
		return "", fmt.Errorf("failed to send header separator: %w", err)
	}

	// Send body
	bodyLines := strings.Split(body, "\n")
	for _, line := range bodyLines {
		// Handle dot-stuffing (lines starting with .)
		if strings.HasPrefix(line, ".") {
			line = "." + line
		}
		_, err := c.writer.PrintfLine(line)
		if err != nil {
			return "", fmt.Errorf("failed to send body line: %w", err)
		}
	}

	// Send termination
	_, err = c.writer.PrintfLine(".")
	if err != nil {
		return "", fmt.Errorf("failed to send termination: %w", err)
	}

	_, _, err = c.reader.ReadCodeLine(240)
	if err != nil {
		return "", fmt.Errorf("server rejected article: %w", err)
	}

	return messageID, nil
}

// JoinGroup joins the specified newsgroup
func (c *Client) JoinGroup(group string) error {
	_, _, err := c.writer.PrintfLine("GROUP %s", group)
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

	_, _ = c.writer.PrintfLine("QUIT")
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
	clients    []*Client
	config     *models.ServerConfig
	maxConns   int
	current    int
	mu         sync.Mutex
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(config *models.ServerConfig, maxConns int) *ConnectionPool {
	return &ConnectionPool{
		config:   config,
		maxConns: maxConns,
		clients:  make([]*Client, 0, maxConns),
	}
}

// GetClient returns an available client from the pool
func (p *ConnectionPool) GetClient() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try to find an available client
	for _, client := range p.clients {
		if client.IsConnected() {
			return client, nil
		}
	}

	// Create new client if we haven't reached max connections
	if len(p.clients) < p.maxConns {
		client := NewClient(p.config)
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