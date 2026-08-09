package nntp

import (
	"bufio"
	"net"
	"net/textproto"
	"strings"
	"testing"
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
