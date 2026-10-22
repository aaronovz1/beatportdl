package securefile

import (
	"encoding/binary"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func accessACL(f *os.File) ([]byte, error) {
	data := make([]byte, 65536)
	n, err := unix.Fgetxattr(int(f.Fd()), "system.posix_acl_access", data)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data[:n], nil
}

// POSIX ACL masks limit named-user and group entries. Reject effective grants
// rather than merely checking the mode bits, including filesystem ACL anomalies.
func checkPOSIXACL(data []byte, sensitive bool) (bool, error) {
	if len(data) == 0 {
		return true, nil
	}
	if len(data) < 4 || (len(data)-4)%8 != 0 || binary.LittleEndian.Uint32(data) != 2 {
		return false, unsafe("unsupported POSIX ACL")
	}
	mask := uint16(7)
	for i := 4; i < len(data); i += 8 {
		if binary.LittleEndian.Uint16(data[i:]) == 0x10 {
			mask = binary.LittleEndian.Uint16(data[i+2:])
		}
	}
	private := true
	for i := 4; i < len(data); i += 8 {
		tag := binary.LittleEndian.Uint16(data[i:])
		perm := binary.LittleEndian.Uint16(data[i+2:])
		id := binary.LittleEndian.Uint32(data[i+4:])
		switch tag {
		case 1:
			continue // Owner permissions are covered by ownership validation.
		case 2:
			perm &= mask
			if id == uint32(os.Geteuid()) || id == 0 {
				continue
			}
		case 4, 8:
			perm &= mask
		case 0x10:
			continue
		case 0x20:
		default:
			return false, unsafe("unknown POSIX ACL entry")
		}
		if perm&2 != 0 {
			return false, unsafe("ACL grants untrusted write access")
		}
		if sensitive && perm&7 != 0 {
			private = false
		}
	}
	return private, nil
}

func checkDirectoryACL(f *os.File, final bool) error {
	data, err := accessACL(f)
	if err != nil {
		return err
	}
	// Mode-authorized writes on a sticky ancestor do not grant permission to
	// replace a trusted-owned child. Named ACL writers obey the same sticky rule.
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !final && info.Mode()&os.ModeSticky != 0 {
		return nil
	}
	_, err = checkPOSIXACL(data, false)
	return err
}

func checkFileACL(f *os.File) (bool, error) {
	data, err := accessACL(f)
	if err != nil {
		return false, err
	}
	return checkPOSIXACL(data, true)
}
