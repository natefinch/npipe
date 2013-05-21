// Copyright 2013 Nate Finch. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Package npipe provides a pure Go wrapper around Windows named pipes.
//
// See http://msdn.microsoft.com/en-us/library/windows/desktop/aa365780
//
// npipe provides an interface based on stdlib's net package, with Dial, Listen, and Accept
// functions, as well as associated implementations of net.Conn and net.Listener
//
// The Dial function connects a client to a named pipe:
//   conn, err := npipe.Dial(`\\.\pipe\mypipename`)
//   if err != nil {
//   	<handle error>
//   }
//   fmt.Fprintf(conn, "Hi server!\n")
//   msg, err := bufio.NewReader(conn).ReadString('\n')
//   ...
//
// The Listen function creates servers:
//
//   ln, err := npipe.Listen(`\\.\pipe\mypipename`)
//   if err != nil {
//   	// handle error
//   }
//   for {
//   	conn, err := ln.Accept()
//   	if err != nil {
//   		// handle error
//   		continue
//   	}
//   	go handleConnection(conn)
//   }
package npipe

//sys createNamedPipe(name *uint16, openMode uint32, pipeMode uint32, maxInstances uint32, outBufSize uint32, inBufSize uint32, defaultTimeout uint32, sa *syscall.SecurityAttributes) (handle syscall.Handle, err error)  [failretval==syscall.InvalidHandle] = CreateNamedPipeW
//sys connectNamedPipe(handle syscall.Handle, overlapped *syscall.Overlapped) (err error) = ConnectNamedPipe
//sys disconnectNamedPipe(handle syscall.Handle) (err error) = DisconnectNamedPipe
//sys waitNamedPipe(name *uint16, timeout uint32) (err error) = WaitNamedPipeW

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

const (
	// openMode
	pipe_access_duplex   = 0x3
	pipe_access_inbound  = 0x1
	pipe_access_outbound = 0x2

	// openMode write flags
	file_flag_first_pipe_instance = 0x00080000
	file_flag_write_through       = 0x80000000
	file_flag_overlapped          = 0x40000000

	// openMode ACL flags
	write_dac              = 0x00040000
	write_owner            = 0x00080000
	access_system_security = 0x01000000

	// pipeMode
	pipe_type_byte    = 0x0
	pipe_type_message = 0x4

	// pipeMode read mode flags
	pipe_readmode_byte    = 0x0
	pipe_readmode_message = 0x2

	// pipeMode wait mode flags
	pipe_wait   = 0x0
	pipe_nowait = 0x1

	// pipeMode remote-client mode flags
	pipe_accept_remote_clients = 0x0
	pipe_reject_remote_clients = 0x8

	pipe_unlimited_instances = 255

	nmpwait_wait_forever = 0xFFFFFFFF

	// this not-an-error that occurs if a client connects to the pipe between
	// the server's CreateNamedPipe and ConnectNamedPipe calls.
	error_pipe_connected syscall.Errno = 0x217
	error_pipe_busy      syscall.Errno = 0xE7
	error_sem_timeout    syscall.Errno = 0x79

	error_bad_pathname syscall.Errno = 0xA1
	error_invalid_name syscall.Errno = 0x7B
)

// PipeError is an error related to a call to a pipe
type PipeError struct {
	msg     string
	timeout bool
}

// Error implements the error interface
func (e PipeError) Error() string {
	return e.msg
}

// Timeout implements net.AddrError.Timeout()
func (e PipeError) Timeout() bool {
	return e.timeout
}

// Temporary implements net.AddrError.Temporary()
func (e PipeError) Temporary() bool {
	return false
}

// Dial connects to a named pipe with the given address. If the specified pipe is not available,
// it will wait indefinitely for the pipe to become available.
//
// The address must be of the form \\.\\pipe\<name> for local pipes and \\<computer>\pipe\<name>
// for remote pipes.
//
// Dial will return a PipeError if you pass in a badly formatted pipe name.
//
// Examples:
//   // local pipe
//   conn, err := Dial(`\\.\pipe\mypipename`)
//
//   // remote pipe
//   conn, err := Dial(`\\othercomp\pipe\mypipename`)
func Dial(address string) (*PipeConn, error) {
	for {
		conn, err := dial(address, nmpwait_wait_forever)
		if err == nil {
			return conn, nil
		}
		if isPipeNotReady(err) {
			<-time.After(100 * time.Millisecond)
			continue
		}
		return nil, err
	}
}

// DialTimeout acts like Dial, but will time out after the duration of timeout
func DialTimeout(address string, timeout time.Duration) (*PipeConn, error) {
	deadline := time.Now().Add(timeout)

	now := time.Now()
	for now.Before(deadline) {
		millis := uint32(deadline.Sub(now) / time.Millisecond)
		conn, err := dial(address, millis)
		if err == nil {
			return conn, nil
		}
		if err == error_sem_timeout {
			// This is WaitNamedPipe's timeout error, so we know we're done
			return nil, PipeError{fmt.Sprintf(
				"Timed out waiting for pipe '%s' to come available", address), true}
		}
		if isPipeNotReady(err) {
			left := deadline.Sub(time.Now())
			retry := 100 * time.Millisecond
			if left > retry {
				<-time.After(retry)
			} else {
				<-time.After(left - time.Millisecond)
			}
			now = time.Now()
			continue
		}
		return nil, err
	}
	return nil, PipeError{fmt.Sprintf(
		"Timed out waiting for pipe '%s' to come available", address), true}
}

// isPipeNotReady checks the error to see if it indicates the pipe is not ready
func isPipeNotReady(err error) bool {
	// Pipe Busy means another client just grabbed the open pipe end,
	// and the server hasn't made a new one yet.
	// File Not Found means the server hasn't created the pipe yet.
	// Neither is a fatal error.

	if err, ok := err.(*os.PathError); ok {
		return err.Err == error_pipe_busy
	}
	return err == syscall.ERROR_FILE_NOT_FOUND
}

// dial is a helper to initiate a connection to a named pipe that has been started by a server.
// The timeout is only enforced if the pipe server has already created the pipe, otherwise
// this function will return immediately.
func dial(address string, timeout uint32) (*PipeConn, error) {
	name, err := syscall.UTF16PtrFromString(string(address))
	if err != nil {
		return nil, err
	}
	// If at least one instance of the pipe has been created, this function
	// will wait timeout milliseconds for it to become available.
	// It will return immediately regardless of timeout, if no instances
	// of the named pipe have been created yet.
	// If this returns with no error, there is a pipe available.
	if err := waitNamedPipe(name, timeout); err != nil {
		if err == error_bad_pathname {
			// badly formatted pipe name
			return nil, badAddr(address)
		}
		return nil, err
	}

	f, err := os.OpenFile(address, os.O_RDWR, os.ModePerm|os.ModeNamedPipe)
	if err != nil {
		return nil, err
	}
	return &PipeConn{file: f, addr: PipeAddr(address)}, nil
}

// New returns a new PipeListener that will listen on a pipe with the given address.
// The address must be of the form \\.\pipe\<name>
//
// Listen will return a PipeError for an incorrectly formatted pipe name.
func Listen(address string) (*PipeListener, error) {
	handle, err := createPipe(address)
	if err == error_invalid_name {
		return nil, badAddr(address)
	}
	if err != nil {
		return nil, err
	}
	return &PipeListener{PipeAddr(address), handle, false}, nil
}

// PipeListener is a named pipe listener. Clients should typically
// use variables of type net.Listener instead of assuming named pipe.
type PipeListener struct {
	addr   PipeAddr
	handle syscall.Handle
	closed bool
}

// Accept implements the Accept method in the net.Listener interface; it
// waits for the next call and returns a generic net.Conn.
func (l *PipeListener) Accept() (net.Conn, error) {
	c, err := l.AcceptPipe()
	if err != nil {
		return nil, err
	}
	return c, nil
}

// AcceptPipe accepts the next incoming call and returns the new connection.
func (l *PipeListener) AcceptPipe() (*PipeConn, error) {
	if l == nil || l.addr == "" || l.closed {
		return nil, syscall.EINVAL
	}

	// the first time we call accept, the handle will have been created by the Listen
	// call. This is to prevent race conditions where the client thinks the server
	// isn't listening because it hasn't actually called create yet. After the first time, we'll
	// have to create a new handle each time
	handle := l.handle
	if handle == 0 {
		var err error
		handle, err = createPipe(string(l.addr))
		if err != nil {
			return nil, err
		}
	} else {
		l.handle = 0
	}

	if err := connectNamedPipe(handle, nil); err != nil && err != error_pipe_connected {
		return nil, err
	}
	f := os.NewFile(uintptr(handle), l.addr.String())
	return &PipeConn{file: f, addr: l.addr}, nil
}

// Close stops listening on the address.
// Already Accepted connections are not closed.
func (l *PipeListener) Close() error {
	if l.closed {
		return nil
	}
	l.closed = true
	if l.handle != 0 {
		err := disconnectNamedPipe(l.handle)
		l.handle = 0
		return err
	}
	return nil
}

// Addr returns the listener's network address, a PipeAddr.
func (l *PipeListener) Addr() net.Addr { return l.addr }

// PipeConn is the implementation of the net.Conn interface for named pipe connections.
type PipeConn struct {
	file *os.File
	addr PipeAddr

	// these aren't actually used yet
	readDeadline  *time.Time
	writeDeadline *time.Time
}

type iodata struct {
	n   int
	err error
}

// Read implements the net.Conn Read method.
func (c *PipeConn) Read(b []byte) (int, error) {
	if c.readDeadline != nil && time.Now().Before(*c.readDeadline) {
		ret := make(chan iodata)
		quit := make(chan bool, 1)
		go c.read(b, ret, quit)
		select {
		case d := <-ret:
			return d.n, d.err
		case <-time.After(c.readDeadline.Sub(time.Now())):
			quit <- true
			syscall.CancelIoEx(syscall.Handle(c.file.Fd()), nil)
			return 0, timeout(c.addr.String())
		}
	}
	return c.file.Read(b)
}

// read is a helper function to support read timeouts
func (c *PipeConn) read(b []byte, ret chan iodata, quit chan bool) {
	n, err := c.file.Read(b)
	select {
	case ret <- iodata{n, err}:
	case <-quit:
	}
}

// Write implements the net.Conn Write method.
func (c *PipeConn) Write(b []byte) (int, error) {
	if c.writeDeadline != nil && time.Now().Before(*c.writeDeadline) {
		ret := make(chan iodata)
		quit := make(chan bool, 1)
		go c.write(b, ret, quit)
		select {
		case d := <-ret:
			return d.n, d.err
		case <-time.After(c.writeDeadline.Sub(time.Now())):
			quit <- true
			syscall.CancelIoEx(syscall.Handle(c.file.Fd()), nil)
			return 0, timeout(c.addr.String())
		}
	}
	return c.file.Write(b)
}

// write is a helper function to support write timeouts
func (c *PipeConn) write(b []byte, ret chan iodata, quit chan bool) {
	n, err := c.file.Write(b)
	select {
	case ret <- iodata{n, err}:
	case <-quit:
	}
}

// Close closes the connection.
func (c *PipeConn) Close() error {
	return c.file.Close()
}

// LocalAddr returns the local network address.
func (c *PipeConn) LocalAddr() net.Addr {
	return c.addr
}

// RemoteAddr returns the remote network address.
func (c *PipeConn) RemoteAddr() net.Addr {
	// not sure what to do here, we don't have remote addr....
	return c.addr
}

// SetDeadline implements the net.Conn SetDeadline method.
// Note that timeouts are only supported on Windows Vista/Server 2008 and above
func (c *PipeConn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	c.SetWriteDeadline(t)
	return nil
}

// SetReadDeadline implements the net.Conn SetReadDeadline method.
// Note that timeouts are only supported on Windows Vista/Server 2008 and above
func (c *PipeConn) SetReadDeadline(t time.Time) error {
	c.readDeadline = &t
	return nil
}

// SetWriteDeadline implements the net.Conn SetWriteDeadline method.
// Note that timeouts are only supported on Windows Vista/Server 2008 and above
func (c *PipeConn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline = &t
	return nil
}

// PipeAddr represents the address of a named pipe.
type PipeAddr string

// Network returns the address's network name, "pipe".
func (a PipeAddr) Network() string { return "pipe" }

// String returns the address of the pipe
func (a PipeAddr) String() string {
	return string(a)
}

// createPipe is a helper function to make sure we always create pipes with the same arguments,
// since subsequent calls to create pipe need to use the same arguments as the first one.
func createPipe(address string) (syscall.Handle, error) {
	n, err := syscall.UTF16PtrFromString(address)
	if err != nil {
		return 0, err
	}

	return createNamedPipe(n,
		pipe_access_duplex,
		pipe_type_byte,
		pipe_unlimited_instances,
		512, 512, 0, nil)
}

func badAddr(addr string) PipeError {
	return PipeError{fmt.Sprintf("Invalid pipe address '%s'.", addr), false}
}
func timeout(addr string) PipeError {
	return PipeError{fmt.Sprintf("Pipe IO timed out waiting for '%s'", addr), true}
}
