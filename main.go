// assuan-relay exposes an Assuan socket-redirect file (as written by
// GnuPG on Windows: a decimal TCP port on line 1 followed by a 16-byte
// binary nonce) as a plain TCP endpoint.
//
// It listens only on the IPv4 loopback address 127.0.0.1, so the relay
// is not reachable from the network.
// For every accepted connection the socket file is re-read (the agent may have
// restarted with a new port and nonce), the nonce is presented to the local
// Assuan server, and the connection is then relayed transparently.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

// nonceSize is the size of the nonce in an Assuan socket file, as
// written by libassuan's _assuan_sock_bind and expected by
// read_port_and_nonce.
const nonceSize = 16

// dialTimeout bounds the connect to the local Assuan server.
const dialTimeout = 5 * time.Second

func main() {
	log.SetFlags(0)
	log.SetPrefix("assuan-relay: ")

	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <tcp-port> <assuan-socket-file>\n", os.Args[0])
		os.Exit(2)
	}
	tcpPort := os.Args[1]
	socketFile := os.Args[2]

	if n, err := strconv.Atoi(tcpPort); err != nil || n < 1 || n > 65535 {
		fmt.Fprintf(os.Stderr, "assuan-relay: invalid TCP port %q\n", tcpPort)
		os.Exit(2)
	}

	// Bind only the IPv4 loopback address.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", tcpPort))
	if err != nil {
		log.Fatalf("listen on 127.0.0.1:%s: %v", tcpPort, err)
	}
	log.Printf("listening on %s, socket file %s", ln.Addr(), socketFile)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		log.Printf("accepted connection from %s", conn.RemoteAddr())
		go handleConn(conn, socketFile)
	}
}

// handleConn services one client connection: it re-reads the socket
// file, connects to the Assuan server described there, presents the
// nonce, and relays traffic until either side closes.
func handleConn(client net.Conn, socketFile string) {
	defer client.Close()

	port, nonce, err := readSocketFile(socketFile)
	if err != nil {
		log.Printf("%s: %v", client.RemoteAddr(), err)
		return
	}

	server, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), dialTimeout)
	if err != nil {
		log.Printf("%s: connect to assuan server 127.0.0.1:%d: %v", client.RemoteAddr(), port, err)
		return
	}
	defer server.Close()

	// Present the nonce; it must be the first payload on the
	// connection, exactly as libassuan's _assuan_sock_connect does.
	if _, err := server.Write(nonce); err != nil {
		log.Printf("%s: sending nonce: %v", client.RemoteAddr(), err)
		return
	}

	relay(client, server)
}

// relay copies bytes in both directions. When one direction sees EOF
// the write side of the other connection is closed, so shutdowns
// propagate cleanly without killing the opposite direction.
func relay(a, b net.Conn) {
	done := make(chan struct{}, 1)
	go func() {
		io.Copy(b, a)
		closeWrite(b)
		done <- struct{}{}
	}()
	io.Copy(a, b)
	closeWrite(a)
	<-done
}

func closeWrite(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		tc.CloseWrite()
	}
}

// readSocketFile parses an Assuan socket-redirect file: a decimal port
// number, a newline, then a 16-byte binary nonce (libassuan's
// read_port_and_nonce format). The file is small, so it is read whole.
func readSocketFile(path string) (port int, nonce []byte, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, err
	}
	if bytes.HasPrefix(data, []byte("!<socket >")) {
		return 0, nil, errors.New("Cygwin-style socket file not supported")
	}
	portLine, rest, found := bytes.Cut(data, []byte("\n"))
	if !found {
		return 0, nil, errors.New("malformed socket file: missing newline after port")
	}
	port, err = strconv.Atoi(string(bytes.TrimSpace(portLine)))
	if err != nil || port < 1 || port > 65535 {
		return 0, nil, errors.New("malformed socket file: invalid port number")
	}
	if len(rest) < nonceSize {
		return 0, nil, errors.New("malformed socket file: nonce too short")
	}
	return port, rest[:nonceSize], nil
}
