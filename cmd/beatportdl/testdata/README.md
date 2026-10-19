# Synthetic Audio Fixtures

These short tones contain no third-party music. Tests read the checked-in files;
FFmpeg is not required to run the direct-download tests.

Generated with FFmpeg using `sine=frequency=440:sample_rate=44100` (or 880 Hz for
`other-tone.flac`), `-t 0.05`, `-fflags +bitexact`, `-flags:a +bitexact`, and
`-map_metadata -1`. FLAC uses `-c:a flac -metadata_header_padding 0`; M4A uses
`-c:a aac`.
