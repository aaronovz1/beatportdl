package securefile

import (
	"encoding/binary"
	"errors"
	"testing"
)

func darwinACLFixture(flags, rights uint32) []byte {
	data := make([]byte, 68)
	binary.LittleEndian.PutUint32(data, 0x012cc16d)
	binary.LittleEndian.PutUint32(data[36:], 1)
	binary.LittleEndian.PutUint32(data[60:], flags)
	binary.LittleEndian.PutUint32(data[64:], rights)
	return data
}

func TestDarwinACLRejectsEveryMutationGrant(t *testing.T) {
	for _, bit := range []uint{2, 4, 5, 6, 8, 10, 12, 13, 21, 23} {
		if _, err := checkDarwinACL(darwinACLFixture(1, 1<<bit), false, false); !errors.Is(err, ErrUnsafe) {
			t.Errorf("accepted write right bit %d: %v", bit, err)
		}
	}
}

func TestDarwinACLDenyReadAndInheritance(t *testing.T) {
	if _, err := checkDarwinACL(darwinACLFixture(2, 0xffffffff), false, true); err != nil {
		t.Errorf("deny-only ACL rejected: %v", err)
	}
	private, err := checkDarwinACL(darwinACLFixture(1, 1<<1), true, false)
	if err != nil || private {
		t.Errorf("broad read ACL = %v, %v", private, err)
	}
	if _, err := checkDarwinACL(darwinACLFixture(1|(1<<5)|(1<<8), 1<<1), false, true); !errors.Is(err, ErrUnsafe) {
		t.Errorf("inheritable read grant accepted: %v", err)
	}
	if _, err := checkDarwinACL([]byte("invalid"), true, false); !errors.Is(err, ErrUnsafe) {
		t.Errorf("unknown ACL accepted: %v", err)
	}
}
