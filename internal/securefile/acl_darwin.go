package securefile

import (
	"encoding/binary"
	"os"
	"runtime"
	stdunsafe "unsafe"

	"golang.org/x/sys/unix"
)

// fgetattrlist exposes the extended ACL without cgo or a pathname-based command.
// Unknown ACL formats fail closed. Deny-only ACLs (common on macOS home folders)
// remain usable; discretionary allow entries are treated conservatively.
func darwinACL(f *os.File) ([]byte, error) {
	attrs := struct {
		Count, Reserved                       uint16
		Common, Volume, Directory, File, Fork uint32
	}{Count: 5, Common: unix.ATTR_CMN_EXTENDED_SECURITY}
	buf := make([]byte, 65536)
	_, _, errno := unix.Syscall6(unix.SYS_FGETATTRLIST, f.Fd(), uintptr(stdunsafe.Pointer(&attrs)), uintptr(stdunsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
	runtime.KeepAlive(f)
	if errno != 0 {
		return nil, errno
	}
	size := int(binary.LittleEndian.Uint32(buf))
	if size < 12 || size > len(buf) {
		return nil, unsafe("invalid extended security attributes")
	}
	offset := int(int32(binary.LittleEndian.Uint32(buf[4:]))) + 4
	length := int(binary.LittleEndian.Uint32(buf[8:]))
	if length == 0 {
		return nil, nil
	}
	if offset < 12 || length < 44 || offset+length > size {
		return nil, unsafe("invalid extended ACL bounds")
	}
	return buf[offset : offset+length], nil
}

func checkDarwinACL(data []byte, sensitive, final bool) (bool, error) {
	if len(data) == 0 {
		return true, nil
	}
	if len(data) < 44 || binary.LittleEndian.Uint32(data) != 0x012cc16d {
		return false, unsafe("unknown extended ACL format")
	}
	count := binary.LittleEndian.Uint32(data[36:])
	if count == 0xffffffff {
		return true, nil
	}
	if count > 128 || int(count)*24+44 > len(data) {
		return false, unsafe("invalid extended ACL entries")
	}
	private := true
	for i := 0; i < int(count); i++ {
		entry := data[44+i*24:]
		flags := binary.LittleEndian.Uint32(entry[16:])
		rights := binary.LittleEndian.Uint32(entry[20:])
		if flags&0xf == 2 {
			continue
		} // Deny ACEs cannot weaken the boundary.
		if flags&0xf != 1 {
			return false, unsafe("unsupported extended ACL entry")
		}
		if final && flags&((1<<5)|(1<<6)) != 0 {
			return false, unsafe("directory ACL grants inherited access")
		}
		if flags&(1<<8) != 0 {
			continue
		} // Inherit-only ACE, not effective here.
		const mutation = (1 << 2) | (1 << 4) | (1 << 5) | (1 << 6) | (1 << 8) | (1 << 10) | (1 << 12) | (1 << 13) | (1 << 21) | (1 << 23)
		if rights&mutation != 0 {
			return false, unsafe("extended ACL grants write access")
		}
		if sensitive && rights != 0 {
			private = false
		}
	}
	return private, nil
}

func checkDirectoryACL(f *os.File, final bool) error {
	data, err := darwinACL(f)
	if err != nil {
		return err
	}
	_, err = checkDarwinACL(data, false, final)
	return err
}

func checkFileACL(f *os.File) (bool, error) {
	data, err := darwinACL(f)
	if err != nil {
		return false, err
	}
	return checkDarwinACL(data, true, false)
}
