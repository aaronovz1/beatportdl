package taglib

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMP4OperationsRejectOtherFormats(t *testing.T) {
	if operation := os.Getenv("BEATPORTDL_TEST_MP4_OPERATION"); operation != "" {
		fileName := filepath.Join(t.TempDir(), "audio.wav")
		if err := os.WriteFile(fileName, minimalWAV(), 0600); err != nil {
			t.Fatal(err)
		}
		file, err := Read(fileName)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if operation == "strip" {
			if err := file.StripMp4(); err == nil {
				t.Fatal("MP4 strip accepted a WAV file")
			}
		} else {
			if err := file.SetItemMp4("BPM", "120"); err == nil {
				t.Fatal("MP4 tag setter accepted a WAV file")
			}
		}
		return
	}
	for _, operation := range []string{"strip", "set"} {
		t.Run(operation, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestMP4OperationsRejectOtherFormats$")
			cmd.Env = append(os.Environ(), "BEATPORTDL_TEST_MP4_OPERATION="+operation)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("native MP4 operation crashed or failed: %v\n%s", err, output)
			}
		})
	}
}
