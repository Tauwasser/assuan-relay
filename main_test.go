package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadSocketFile(t *testing.T) {
	nonce := bytes.Repeat([]byte{0xAB}, nonceSize)
	tests := []struct {
		name    string
		content []byte
		port    int
		err     string
	}{
		{
			name:    "valid",
			content: append([]byte("3423\n"), nonce...),
			port:    3423,
		},
		{
			name:    "carriage return after port",
			content: append([]byte("3423\r\n"), nonce...),
			port:    3423,
		},
		{
			name:    "trailing data after nonce ignored",
			content: append(append([]byte("3423\n"), nonce...), []byte("junk")...),
			port:    3423,
		},
		{
			name:    "cygwin-style socket file",
			content: []byte("!<socket >4 p ABCDEF\n"),
			err:     "Cygwin-style socket file not supported",
		},
		{
			name:    "missing newline",
			content: []byte("3423"),
			err:     "missing newline after port",
		},
		{
			name:    "non-numeric port",
			content: append([]byte("abcd\n"), nonce...),
			err:     "invalid port number",
		},
		{
			name:    "port out of range",
			content: append([]byte("65536\n"), nonce...),
			err:     "invalid port number",
		},
		{
			name:    "nonce too short",
			content: []byte("3423\nshort"),
			err:     "nonce too short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "socket")
			if err := os.WriteFile(path, tt.content, 0o600); err != nil {
				t.Fatal(err)
			}

			port, gotNonce, err := readSocketFile(path)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("readSocketFile() error = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("readSocketFile() unexpected error: %v", err)
			}
			if port != tt.port {
				t.Errorf("port = %d, want %d", port, tt.port)
			}
			if !bytes.Equal(gotNonce, nonce) {
				t.Errorf("nonce = % x, want % x", gotNonce, nonce)
			}
		})
	}
}

// TestHandleConn exercises the full relay path: a fake Assuan server
// verifies that the nonce arrives as the first payload, then echoes a
// payload back through the relay to the client.
func TestHandleConn(t *testing.T) {
	wantNonce := bytes.Repeat([]byte{0x42}, nonceSize)

	serverLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer serverLn.Close()

	type serverResult struct {
		nonce []byte
		err   error
	}
	serverDone := make(chan serverResult, 1)
	go func() {
		conn, err := serverLn.Accept()
		if err != nil {
			serverDone <- serverResult{nil, err}
			return
		}
		defer conn.Close()
		got := make([]byte, nonceSize)
		if _, err := io.ReadFull(conn, got); err != nil {
			serverDone <- serverResult{nil, err}
			return
		}
		// Echo one payload round-trip so the client can observe it.
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			serverDone <- serverResult{got, err}
			return
		}
		if _, err := conn.Write(buf); err != nil {
			serverDone <- serverResult{got, err}
			return
		}
		serverDone <- serverResult{got, nil}
	}()

	port := serverLn.Addr().(*net.TCPAddr).Port
	socketFile := filepath.Join(t.TempDir(), "S.gpg-agent")
	content := append([]byte(strconv.Itoa(port)+"\n"), wantNonce...)
	if err := os.WriteFile(socketFile, content, 0o600); err != nil {
		t.Fatal(err)
	}

	clientLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer clientLn.Close()
	go func() {
		conn, err := clientLn.Accept()
		if err != nil {
			return
		}
		handleConn(conn, socketFile)
	}()

	client, err := net.Dial("tcp", clientLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	echo := make([]byte, 4)
	if _, err := io.ReadFull(client, echo); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(echo) != "ping" {
		t.Errorf("echo = %q, want %q", echo, "ping")
	}

	res := <-serverDone
	if res.err != nil {
		t.Fatalf("fake assuan server: %v", res.err)
	}
	if !bytes.Equal(res.nonce, wantNonce) {
		t.Errorf("server received nonce = % x, want % x", res.nonce, wantNonce)
	}
}
