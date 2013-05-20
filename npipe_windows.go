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

//sys create(name *uint16, openMode uint32, pipeMode uint32, maxInstances uint32, outBufSize uint32, inBufSize uint32, defaultTimeout uint32, sa *syscall.SecurityAttributes) (handle syscall.Handle, err error)  [failretval==syscall.InvalidHandle] = CreateNamedPipeW
//sys connect(handle syscall.Handle, overlapped *syscall.Overlapped) (err error) = ConnectNamedPipe
//sys disconnect(handle syscall.Handle) (err error) = DisconnectNamedPipe
//sys wait(name *uint16, timeout uint32) (err error) = WaitNamedPipeW

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

type PipeError struct {
	address string
}

func (e PipeError) Error() string {
	return fmt.Sprintf("Invalid pipe address '%s'", e.address)
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
	name, err := syscall.UTF16PtrFromString(string(address))
	if err != nil {
		return nil, err
	}
	for {
		// this will fail on badly formatted pipe names
		if err := wait(name, nmpwait_wait_forever); err != nil {
			if err == error_bad_pathname {
				return nil, PipeError{address}
			}
			return nil, err
		}
		f, err := os.OpenFile(address, os.O_RDWR, os.ModePerm|os.ModeNamedPipe)

		if err == nil {
			return &PipeConn{file: f, addr: PipeAddr(address)}, nil
		}

		// pipe busy means another client just grabbed the open pipe end, and the server hasn't made
		// a new one yet.
		if err.(*os.PathError).Err != error_pipe_busy {
			return nil, err
		}
	}
}

// New returns a new PipeListener that will listen on a pipe with the given address.
// The address must be of the form \\.\pipe\<name>
//
// Listen will return a PipeError for an incorrectly formatted pipe name.
func Listen(address string) (*PipeListener, error) {
	handle, err := createPipe(address)
	if err == error_invalid_name {
		return nil, PipeError{address}
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

	if err := connect(handle, nil); err != nil && err != error_pipe_connected {
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
		err := disconnect(l.handle)
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
	readDeadline  time.Time
	writeDeadline time.Time
}

// Read implements the net.Conn Read method.
func (c *PipeConn) Read(b []byte) (int, error) {
	return c.file.Read(b)
}

// Write implements the net.Conn Write method.
func (c *PipeConn) Write(b []byte) (int, error) {
	return c.file.Write(b)
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
func (c *PipeConn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	c.SetWriteDeadline(t)
	return nil
}

// SetReadDeadline implements the net.Conn SetReadDeadline method.
func (c *PipeConn) SetReadDeadline(t time.Time) error {
	c.readDeadline = t
	return nil
}

// SetWriteDeadline implements the net.Conn SetWriteDeadline method.
func (c *PipeConn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline = t
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

	return create(n, pipe_access_duplex, pipe_type_byte, pipe_unlimited_instances, 512, 512, 0, nil)
}
