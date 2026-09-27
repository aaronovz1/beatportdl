package taglib

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestGetPropertyReturnsEmptyStringWhenPropertyIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-tags.wav")
	if err := os.WriteFile(path, minimalWAV(), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}

	file, err := Read(path)
	if err != nil {
		t.Fatalf("Read(%s): %v", path, err)
	}
	defer file.Close()

	if got := file.GetProperty("TRACK_ID"); got != "" {
		t.Fatalf("GetProperty(TRACK_ID) = %q, want empty string", got)
	}
}

func minimalWAV() []byte {
	data := make([]byte, 46)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 1)
	binary.LittleEndian.PutUint32(data[24:28], 8000)
	binary.LittleEndian.PutUint32(data[28:32], 16000)
	binary.LittleEndian.PutUint16(data[32:34], 2)
	binary.LittleEndian.PutUint16(data[34:36], 16)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], 2)
	return data
}
