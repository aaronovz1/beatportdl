package config

import "testing"

func TestDefaultTagMappingsM4ABPMUsesRawItem(t *testing.T) {
	if got := DefaultTagMappings["m4a"]["track_bpm"]; got != "BPM_raw" {
		t.Fatalf("DefaultTagMappings[m4a][track_bpm] = %q, want BPM_raw", got)
	}
}

func TestDefaultTagMappingsFLACBPMUsesVorbisProperty(t *testing.T) {
	if got := DefaultTagMappings["flac"]["track_bpm"]; got != "BPM" {
		t.Fatalf("DefaultTagMappings[flac][track_bpm] = %q, want BPM", got)
	}
}

func TestValidateTagMappingsRejectsUnknownField(t *testing.T) {
	mappings := map[string]map[string]string{
		"m4a": {
			"not_a_real_field": "BPM_raw",
		},
	}

	if err := ValidateTagMappings(mappings); err == nil {
		t.Fatal("ValidateTagMappings() returned nil for an unknown field")
	}
}
