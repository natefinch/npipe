npipe  [![Build status](https://ci.appveyor.com/api/projects/status/00vuepirsot29qwi)](https://ci.appveyor.com/project/natefinch/npipe) [![GoDoc](https://godoc.org/gopkg.in/natefinch/npipe.v2?status.svg)](https://godoc.org/gopkg.in/natefinch/npipe.v2)
=====
A Windows named pipe implementation written in pure Go.

Documentation at http://godoc.org/gopkg.in/natefinch/npipe.v2

Windows Pipe documentation at http://msdn.microsoft.com/en-us/library/windows/desktop/aa365780

Note that the code lives at github.com/natefinch/npipe (v2 branch) but should be
imported as gopkg.in/natefinch/npipe.v2 (the package name is still npipe).

Implements the net.Conn interface and supports rpc over the connection.

### Notes 

* Deadlines for reading/writing to the connection are only functional in Windows Vista/Server 2008 and above, due to limitations with the Windows API.
* The pipes support byte mode only (no support for message mode)

### How to Build
go get gopkg.in/natefinch/npipe.v2