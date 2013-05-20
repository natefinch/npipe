package npipe

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

const (
	clientMsg = "Hi server!\n"
	serverMsg = "Hi there, client!\n"

	fn = `C:\62DA0493-99A1-4327-B5A8-6C4E4466C3FC.txt`
)

// TestBadDial tests that if you dial something other than a valid pipe path, that you get back a PipeError
// and that you don't accidently create a file on disk (since dial uses OpenFile)
func TestBadDial(t *testing.T) {
	ns := []string{fn, "http://www.google.com", "somethingbadhere"}
	for _, n := range ns {
		c, err := Dial(n)
		if _, ok := err.(PipeError); !ok {
			t.Errorf("Dialing invalid pipe address '%s' did not result in correct error! Expected PipeError, got '%v'", n, err)
		}
		if c != nil {
			t.Errorf("Dialing invalid pipe address '%s' returned non-nil connection", n)
		}
		if b, _ := exists(n); b {
			t.Errorf("Dialing invalid pipe address '%s' incorrectly created file on disk", n)
		}
	}
}

// TestDialExistingFile tests that if you dial with the name of an existing file,
// that you don't accidentally open the file (since dial uses OpenFile)
func TestDialExistingFile(t *testing.T) {
	if f, err := os.Create(fn); err != nil {
		t.Fatalf("Unexpected error creating file '%s': '%v'", fn, err)
	} else {
		// we don't actually need to write to the file, just need it to exist
		f.Close()
		defer os.Remove(fn)
	}
	c, err := Dial(fn)
	if _, ok := err.(PipeError); !ok {
		t.Errorf("Dialing invalid pipe address '%s' did not result in error! Expected PipeError, got '%v'", fn, err)
	}
	if c != nil {
		t.Errorf("Dialing invalid pipe address '%s' returned non-nil connection", fn)
	}
}

// TestBadListen tests that if you listen on a bad address, that we get back ERROR_INVALID_NAME
func TestBadListen(t *testing.T) {
	addr := "not a valid pipe address"
	ln, err := Listen(addr)
	if _, ok := err.(PipeError); !ok {
		t.Errorf("Listening on invalid pipe address '%s' did not result in correct error! Expected PipeError, got '%v'", addr, err)
	}
	if ln != nil {
		t.Error("Listening on a bad address '%s' returned non-nil listener.", addr)
	}
}

// TestCommonUseCase is a full run-through of the most common use case, where you create a listener
// and then dial into it with several clients in succession
func TestCommonUseCase(t *testing.T) {
	address := `\\.\pipe\mypipe`
	convos := 5
	clients := 10

	done := make(chan bool)
	quit := make(chan bool)

	go aggregateDones(done, quit, clients)

	ln, err := Listen(address)
	if err != nil {
		t.Fatal("Error starting to listen on pipe: ", err)
	}

	for x := 0; x < clients; x++ {
		go startClient(address, done, convos, t)
	}

	go startServer(ln, convos, t)

	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("Failed to receive quit message after a reasonable timeout")
	}
}

// aggregateDones simply aggregates messages from the done channel
// until it sees total, and then sends a message on the quit channel
func aggregateDones(done, quit chan bool, total int) {
	dones := 0
	for dones < total {
		<-done
		dones++
	}
	quit <- true
}

// startServer accepts connections and spawns goroutines to handle them
func startServer(ln *PipeListener, iter int, t *testing.T) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			t.Fatal("Error accepting connection: ", err)
		}
		go handleConnection(conn, iter, t)
	}
}

// handleConnection is the goroutine that handles connections on the server side
// it expects to read a message and then write a message, convos times, before exiting.
func handleConnection(conn net.Conn, convos int, t *testing.T) {
	r := bufio.NewReader(conn)
	for x := 0; x < convos; x++ {
		msg, err := r.ReadString('\n')
		if err != nil {
			t.Fatal("Error reading from server connection: ", err)
		}
		if msg != clientMsg {
			t.Fatalf("Read incorrect message from client. Expected '%s', got '%s'", clientMsg, msg)
		}

		if _, err := fmt.Fprint(conn, serverMsg); err != nil {
			t.Fatal("Error on server writing to pipe: ", err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal("Error closing server side of connection: ", err)
	}
}

// startClient waits on a pipe at the given address. It expects to write a message and then
// read a message from the pipe, convos times, and then sends a message on the done
// channel
func startClient(address string, done chan bool, convos int, t *testing.T) {
	c := make(chan *PipeConn)
	go dial(address, c, t)

	var conn *PipeConn
	select {
	case conn = <-c:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Client timed out waiting for dial to resolve")
	}
	r := bufio.NewReader(conn)
	for x := 0; x < convos; x++ {
		if _, err := fmt.Fprint(conn, clientMsg); err != nil {
			t.Fatal("Error on client writing to pipe: ", err)
		}

		msg, err := r.ReadString('\n')
		if err != nil {
			t.Fatal("Error reading from client connection: ", err)
		}
		if msg != serverMsg {
			t.Fatalf("Read incorrect message from server. Expected '%s', got '%s'", serverMsg, msg)
		}
	}

	if err := conn.Close(); err != nil {
		t.Fatal("Error closing client side of pipe", err)
	}
	done <- true
}

// dial is a helper that dials and returns the connection on the given channel.
// this is useful for being able to give dial a timeout
func dial(address string, c chan *PipeConn, t *testing.T) {
	conn, err := Dial(address)
	if err != nil {
		t.Fatal("Error from dial: ", err)
	}
	c <- conn
}

// exists is a simple helper function to detect if a file exists on disk
func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
