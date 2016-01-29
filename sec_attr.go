package npipe

import (
	"os"
	"syscall"
	"unsafe"
)

const SECURITY_DESCRIPTOR_REVISION = 1

var (
	advapi32                         = syscall.NewLazyDLL("advapi32.dll")
	procInitializeSecurityDescriptor = advapi32.NewProc("InitializeSecurityDescriptor")
	procSetSecurityDescriptorDacl    = advapi32.NewProc("SetSecurityDescriptorDacl")
)

func initSecurityAttributes() (*syscall.SecurityAttributes, error) {

	// create security descriptor
	sd := make([]byte, 4096)
	if res, _, err := procInitializeSecurityDescriptor.Call(
		uintptr(unsafe.Pointer(&sd[0])),
		SECURITY_DESCRIPTOR_REVISION); int(res) == 0 {

		return nil, os.NewSyscallError("InitializeSecurityDescriptor", err)
	}

	// configure security descriptor
	present := 1
	defaulted := 0
	if res, _, err := procSetSecurityDescriptorDacl.Call(
		uintptr(unsafe.Pointer(&sd[0])),
		uintptr(present),
		uintptr(unsafe.Pointer(nil)), // acl
		uintptr(defaulted)); int(res) == 0 {

		return nil, os.NewSyscallError("SetSecurityDescriptorDacl", err)
	}

	var sa syscall.SecurityAttributes
	sa.Length = uint32(unsafe.Sizeof(sa))
	sa.SecurityDescriptor = uintptr(unsafe.Pointer(&sd[0]))

	return &sa, nil

}
