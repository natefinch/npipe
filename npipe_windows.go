// Copyright 2013 Nate Finch. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Package npipe provides a pure Go wrapper around Windows Named Pipes.
// See http://msdn.microsoft.com/en-us/library/windows/desktop/aa365780
package npipe

//sys create(name *uint16, openMode uint32, pipeMode uint32, maxInstances uint32, outBufSize uint32, inBufSize uint32, defaultTimeout uint32, sa *syscall.SecurityAttributes) (handle syscall.Handle, err error)  [failretval==syscall.InvalidHandle] = CreateNamedPipeW
//sys connect(handle syscall.Handle, overlapped *syscall.Overlapped) (err error) = ConnectNamedPipe
//sys disconnect(handle syscall.Handle) (err error) = DisconnectNamedPipe
//sys wait(name *uint16, timeout uint32) (err error) = WaitNamedPipeW

import (
	"net"
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
	// the server's CreateNamedPipe and ConnectNamedPipe calls
	error_pipe_connected syscall.Errno = 0x217
	error_pipe_busy      syscall.Errno = 0xE7
	error_sem_timeout    syscall.Errno = 0x79
)

// PipeAddr represents the address of a named pipe.
type PipeAddr string

// Network returns the address's network name, "pipe".
func (a PipeAddr) Network() string { return "pipe" }

func (a PipeAddr) String() string {
	return string(a)
}

// PipeListener is a named pipe listener. Clients should typically
// use variables of type Listener instead of assuming named pipe.
type PipeListener struct {
	addr   PipeAddr
	handle syscall.Handle
	closed bool
}

// New returns a new PipeListener that will listen on a pipe with the given name
func Listen(address string) (*PipeListener, error) {
	handle, err := createPipe(address)
	if err != nil {
		return nil, err
	}
	return &PipeListener{PipeAddr(address), handle, false}, nil
}

// AcceptPipe accepts the next incoming call and returns the new
// connection
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
	return &PipeConn{handle, l.addr, true}, nil
}

// Accept implements the Accept method in the Listener interface; it
// waits for the next call and returns a generic Conn.
func (l *PipeListener) Accept() (net.Conn, error) {
	c, err := l.AcceptPipe()
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Close stops listening on the address.
// Already Accepted connections are not closed.
func (l *PipeListener) Close() error {
	l.closed = true
	return nil
}

// Addr returns the listener's network address, a PipeAddr.
func (l *PipeListener) Addr() net.Addr { return l.addr }

type PipeConn struct {
	handle   syscall.Handle
	addr     PipeAddr
	isserver bool
}

func (c *PipeConn) Read(b []byte) (int, error) {
	return syscall.Read(c.handle, b)
}

func (c *PipeConn) Write(b []byte) (int, error) {
	return syscall.Write(c.handle, b)
}

func (c *PipeConn) Close() error {
	// not really sure what the difference is
	if c.isserver {
		return disconnect(c.handle)
	}
	return syscall.Close(c.handle)
}

// LocalAddr returns the local network address.
func (c *PipeConn) LocalAddr() net.Addr {
	return c.addr
}

func (c *PipeConn) RemoteAddr() net.Addr {
	// not sure what to do here, we don't have remote addr....
	return c.addr
}

func (c *PipeConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *PipeConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *PipeConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func Dial(address string) (*PipeConn, error) {
	name, err := syscall.UTF16PtrFromString(string(address))
	if err != nil {
		return nil, err
	}
	for {
		handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
		if err == nil {
			return &PipeConn{handle, PipeAddr(address), false}, nil
		}
		if err != error_pipe_busy {
			return nil, err
		}
		if err := wait(name, nmpwait_wait_forever); err != nil {
			return nil, err
		}
	}
}

func createPipe(address string) (syscall.Handle, error) {
	name, err := syscall.UTF16PtrFromString(address)
	if err != nil {
		return 0, err
	}

	return create(name, pipe_access_duplex, pipe_type_byte, pipe_unlimited_instances, 512, 512, 1000, nil)
}
