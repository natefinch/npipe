npipe
=====
!!! This package is now deprecated and not under active development.  See the v2
branch in github for the currently supported branch. !!!

A Windows named pipe implementation written in pure Go.

Documentation at http://godoc.org/github.com/natefinch/npipe

Windows Pipe documentation at http://msdn.microsoft.com/en-us/library/windows/desktop/aa365780

Written for Go 1.1.

### Notes
* Deadlines for reading/writing to the connection are only functional in Windows Vista/Server 2008 and above, due to limitations with the Windows API.
* The pipes support byte mode only (no support for message mode)

### How to Build
go get github.com/natefinch/npipe

If you need to add or change the syscalls that have been defined in npipe_windows.go, you'll need to regenerate the z files by running:

$gosource/src/pkg/syscall/mksyscall_windows.pl npipe_windows.go > znpipe_windows_amd64.go
$gosource/src/pkg/syscall/mksyscall_windows.pl -l32 npipe_windows.go > znpipe_windows_386.go

