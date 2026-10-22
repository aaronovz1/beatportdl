package securefile

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsProtectedOwnerSystemDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := Write(path, []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sd, sid, err := descriptor(f)
	if err != nil {
		t.Fatal(err)
	}
	private, err := checkDescriptor(sd, sid, true, true)
	if err != nil || !private {
		t.Fatalf("private DACL = %v, %v", private, err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if acl.AceCount != 2 {
		t.Fatalf("ACE count = %d, want owner and SYSTEM only", acl.AceCount)
	}
}

func TestWindowsDescriptorRejectsUntrustedMutationAndOwner(t *testing.T) {
	sid, err := currentSID()
	if err != nil {
		t.Fatal(err)
	}
	for _, sddl := range []string{
		"O:" + sid.String() + "D:P(A;;FA;;;WD)",
		"O:SYD:P(A;;FA;;;" + sid.String() + ")",
		"O:" + sid.String() + "D:NO_ACCESS_CONTROL",
	} {
		sd, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := checkDescriptor(sd, sid, true, true); err == nil {
			t.Errorf("accepted unsafe descriptor %s", sddl)
		}
	}
}

func TestWindowsBroadReadDACLIsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := Write(path, []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	sid, err := currentSID()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;;FA;;;" + sid.String() + ")(A;;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	data, err := Read(path)
	if err != nil || string(data) != "synthetic" {
		t.Fatalf("Read = %q, %v", data, err)
	}
	after, _ := os.Stat(path)
	if os.SameFile(before, after) {
		t.Error("Read changed ACL on original inode")
	}
	if err := Validate(path); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsRejectsAlternateStreams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials:stream")
	if err := Write(path, []byte("synthetic")); err == nil {
		t.Error("accepted alternate data stream")
	}
}
