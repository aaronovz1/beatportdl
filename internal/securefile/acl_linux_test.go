package securefile

import (
	"encoding/binary"
	"errors"
	"testing"
)

func posixACLFixture(mask, namedPerm uint16) []byte {
	data := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(data, 2)
	tags := []uint16{1, 2, 4, 0x10, 0x20}
	perms := []uint16{7, namedPerm, 0, mask, 0}
	for i, tag := range tags {
		entry := data[4+i*8:]
		binary.LittleEndian.PutUint16(entry, tag)
		binary.LittleEndian.PutUint16(entry[2:], perms[i])
		binary.LittleEndian.PutUint32(entry[4:], 0x7ffffffe)
	}
	return data
}

func TestLinuxACLRejectsEffectiveNamedWriteGrant(t *testing.T) {
	if _, err := checkPOSIXACL(posixACLFixture(2, 2), false); !errors.Is(err, ErrUnsafe) {
		t.Errorf("named write grant = %v", err)
	}
	if _, err := checkPOSIXACL(posixACLFixture(0, 2), false); err != nil {
		t.Errorf("masked-out grant rejected: %v", err)
	}
	private, err := checkPOSIXACL(posixACLFixture(4, 4), true)
	if err != nil || private {
		t.Errorf("named read grant = %v, %v", private, err)
	}
	if _, err := checkPOSIXACL([]byte("invalid"), true); !errors.Is(err, ErrUnsafe) {
		t.Errorf("invalid ACL = %v", err)
	}
}
