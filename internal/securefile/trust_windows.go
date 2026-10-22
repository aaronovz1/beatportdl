package securefile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	stdunsafe "unsafe"

	"golang.org/x/sys/windows"
)

func splitAbsolute(path string) (string, []string, error) {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' {
		return "", nil, unsafe("UNC and device paths are not supported for sensitive files")
	}
	parts := strings.FieldsFunc(strings.TrimPrefix(path, volume), func(r rune) bool { return r == '\\' || r == '/' })
	return volume + `\`, parts, nil
}

func checkName(name string) error {
	if strings.ContainsAny(name, ":") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return unsafe("ambiguous Windows filename or alternate data stream")
	}
	return nil
}

func checkAlias(os.FileInfo) error {
	return unsafe("Windows directory reparse points are not accepted")
}

func currentSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid.Copy()
}

func privilegedSID(sid, current *windows.SID) bool {
	if sid.Equals(current) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return true
	}
	// Windows system ancestors may be owned by the Windows Modules Installer.
	return sid.String() == "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
}

func handleInfo(f *os.File) (*windows.ByHandleFileInformation, error) {
	h := windows.Handle(f.Fd())
	kind, err := windows.GetFileType(h)
	if err != nil {
		return nil, err
	}
	if kind != windows.FILE_TYPE_DISK {
		return nil, unsafe("not a disk file")
	}
	info := new(windows.ByHandleFileInformation)
	if err := windows.GetFileInformationByHandle(h, info); err != nil {
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, unsafe("reparse point is not accepted")
	}
	return info, nil
}

// Windows ACLs are evaluated conservatively: deny ACEs do not cancel a dangerous
// allow ACE, and unsupported conditional/object ACEs fail closed.
func checkDescriptor(sd *windows.SECURITY_DESCRIPTOR, current *windows.SID, sensitive, final bool) (bool, error) {
	owner, _, err := sd.Owner()
	if err != nil {
		return false, err
	}
	if owner == nil || (sensitive && !owner.Equals(current)) || (!sensitive && !privilegedSID(owner, current)) {
		return false, unsafe("untrusted Windows owner")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return false, unsafe("missing or unrestricted Windows DACL")
	}
	control, _, err := sd.Control()
	if err != nil {
		return false, err
	}
	private := control&windows.SE_DACL_PROTECTED != 0
	const mutation = windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | 0x40 | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.GENERIC_ALL | windows.GENERIC_WRITE
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return false, err
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false, unsafe("unsupported Windows ACE")
		}
		sid := (*windows.SID)(stdunsafe.Pointer(&ace.SidStart))
		mask := uint32(ace.Mask)
		if !privilegedSID(sid, current) {
			dangerous := uint32(mutation)
			if !sensitive && !final {
				// Creating a new sibling is not replacing the already selected,
				// trusted-owned child. Windows volume roots often permit this.
				dangerous &^= windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA
			}
			if mask&dangerous != 0 {
				return false, unsafe("DACL grants untrusted modification access")
			}
		}
		if sensitive && (!sid.Equals(current) && !sid.IsWellKnown(windows.WinLocalSystemSid)) && mask != 0 {
			private = false
		}
		if sensitive && ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			private = false
		}
	}
	return private, nil
}

func descriptor(f *os.File) (*windows.SECURITY_DESCRIPTOR, *windows.SID, error) {
	sid, err := currentSID()
	if err != nil {
		return nil, nil, err
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	return sd, sid, err
}

func checkDirectoryHandle(f *os.File, final bool) error {
	info, err := handleInfo(f)
	if err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return unsafe("not a directory")
	}
	sd, sid, err := descriptor(f)
	if err != nil {
		return err
	}
	_, err = checkDescriptor(sd, sid, false, final)
	return err
}

func checkFileHandle(f *os.File, info os.FileInfo) (bool, error) {
	if !info.Mode().IsRegular() {
		return false, unsafe("not a regular file")
	}
	winInfo, err := handleInfo(f)
	if err != nil {
		return false, err
	}
	if winInfo.NumberOfLinks != 1 {
		return false, unsafe("file has multiple hard links")
	}
	sd, sid, err := descriptor(f)
	if err != nil {
		return false, err
	}
	return checkDescriptor(sd, sid, true, true)
}

func protectedDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	sid, err := currentSID()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;;FA;;;SY)(A;;FA;;;" + sid.String() + ")")
}

func nativeOpen(d *directory, name string, access, disposition, options uint32, sd *windows.SECURITY_DESCRIPTOR) (*os.File, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	n, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	oa := &windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(d.file.Fd()), ObjectName: n, Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE, SecurityDescriptor: sd}
	oa.Length = uint32(stdunsafe.Sizeof(*oa))
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE|windows.READ_CONTROL, oa, &windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, disposition, options|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	runtime.KeepAlive(sd)
	runtime.KeepAlive(d)
	if err != nil {
		var status windows.NTStatus
		if errors.As(err, &status) {
			err = status.Errno()
		}
		return nil, &os.PathError{Op: "NtCreateFile", Path: name, Err: err}
	}
	return os.NewFile(uintptr(handle), name), nil
}

func openLeaf(d *directory, name string, appendMode bool) (*os.File, error) {
	access := uint32(windows.FILE_GENERIC_READ)
	if appendMode {
		access = windows.FILE_APPEND_DATA | windows.FILE_READ_ATTRIBUTES
	}
	return nativeOpen(d, name, access, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
}

func createLeaf(d *directory, name string) (*os.File, error) {
	sd, err := protectedDescriptor()
	if err != nil {
		return nil, err
	}
	return nativeOpen(d, name, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE, sd)
}

func makeDirectory(d *directory, name string) error {
	sd, err := protectedDescriptor()
	if err != nil {
		return err
	}
	f, err := nativeOpen(d, name, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_CREATE, windows.FILE_DIRECTORY_FILE, sd)
	if err != nil {
		return err
	}
	return f.Close()
}
