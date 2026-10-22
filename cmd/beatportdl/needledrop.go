package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	m3u8 "github.com/Eyevinn/hls-m3u8/m3u8"
	"github.com/vbauerster/mpb/v8"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unspok3n/beatportdl/internal/securefile"
)

const (
	maxHLSManifestBytes = 1024 * 1024
	maxHLSSegmentBytes  = 64 * 1024 * 1024
	maxHLSKeyBytes      = 32
)

func readHLSBody(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("HLS response exceeds %d bytes", limit)
	}
	return data, nil
}

type StreamKey struct {
	Value []byte
	IV    []byte
}

func (app *application) getStreamSegments(stream string) (*[]string, *StreamKey, error) {
	resp, err := app.getMedia(stream)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}
	manifest, err := readHLSBody(resp.Body, maxHLSManifestBytes)
	if err != nil {
		return nil, nil, err
	}
	playlist, _, err := m3u8.DecodeFrom(bytes.NewReader(manifest), true)
	if err != nil {
		return nil, nil, err
	}
	media, ok := playlist.(*m3u8.MediaPlaylist)
	if !ok {
		return nil, nil, fmt.Errorf("stream playlist is not a media playlist")
	}
	u, err := url.Parse(stream)
	if err != nil {
		return nil, nil, err
	}
	resolve := func(reference string) (string, error) {
		child, err := url.Parse(reference)
		if err != nil {
			return "", err
		}
		resolved := u.ResolveReference(child)
		if resolved.Scheme != "https" && resolved.Scheme != "http" || resolved.Host == "" || resolved.User != nil {
			return "", errors.New("invalid HLS resource URL")
		}
		return resolved.String(), nil
	}
	var segments []string
	var streamKey StreamKey
	var firstKey *m3u8.Key
	for i, segment := range media.Segments {
		if segment == nil {
			break
		}
		if segment.URI == "" {
			return nil, nil, fmt.Errorf("stream segment %d has empty URI", i)
		}
		if i == 0 {
			for keyIndex := range segment.Keys {
				candidate := &segment.Keys[keyIndex]
				if candidate.Method == "AES-128" && (candidate.Keyformat == "" || candidate.Keyformat == "identity") {
					firstKey = candidate
					break
				}
			}
			if firstKey == nil || firstKey.URI == "" {
				return nil, nil, fmt.Errorf("stream segment %d is missing an encryption key URI", i)
			}
			if firstKey.IV == "" {
				return nil, nil, fmt.Errorf("stream segment %d is missing an encryption key IV", i)
			}
			keyURL, err := resolve(firstKey.URI)
			if err != nil {
				return nil, nil, err
			}
			req, err := app.getMedia(keyURL)
			if err != nil {
				return nil, nil, err
			}
			defer req.Body.Close()
			if req.StatusCode != http.StatusOK {
				return nil, nil, fmt.Errorf("get stream key failed with status code: %d", req.StatusCode)
			}
			keyBytes, err := readHLSBody(req.Body, maxHLSKeyBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("read stream key: %w", err)
			}
			ivBytes, err := hex.DecodeString(strings.TrimPrefix(firstKey.IV, "0x"))
			if err != nil {
				return nil, nil, fmt.Errorf("decode stream iv: %v", err)
			}
			streamKey.Value = keyBytes
			streamKey.IV = ivBytes
		} else {
			for _, key := range segment.Keys {
				if key.Method != firstKey.Method || key.URI != firstKey.URI || key.IV != firstKey.IV || key.Keyformat != firstKey.Keyformat {
					return nil, nil, errors.New("HLS encryption key changes are not supported")
				}
			}
		}
		segmentURL, err := resolve(segment.URI)
		if err != nil {
			return nil, nil, err
		}
		segments = append(segments, segmentURL)
	}

	return &segments, &streamKey, nil
}

func decryptSegment(segment []byte, key StreamKey) ([]byte, error) {
	block, err := aes.NewCipher(key.Value)
	if err != nil {
		return nil, err
	}
	if len(key.IV) != block.BlockSize() {
		return nil, fmt.Errorf("invalid stream IV length: got %d bytes, want %d", len(key.IV), block.BlockSize())
	}
	if len(segment) == 0 || len(segment)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("invalid encrypted segment length: %d bytes", len(segment))
	}
	cbc := cipher.NewCBCDecrypter(block, key.IV)
	decrypted := make([]byte, len(segment))
	cbc.CryptBlocks(decrypted, segment)
	padding := int(decrypted[len(decrypted)-1])
	if padding == 0 || padding > block.BlockSize() {
		return nil, errors.New("invalid encrypted segment padding")
	}
	for _, value := range decrypted[len(decrypted)-padding:] {
		if int(value) != padding {
			return nil, errors.New("invalid encrypted segment padding")
		}
	}
	return decrypted[:len(decrypted)-padding], nil
}

func (app *application) downloadSegments(directory string, segmentUrls []string, key StreamKey, pbPrefix string) (string, error) {
	if err := app.context().Err(); err != nil {
		return "", err
	}
	if len(segmentUrls) == 0 {
		return "", errors.New("stream has no segments")
	}
	file, err := os.CreateTemp(directory, ".beatportdl-segments-*.ts")
	if err != nil {
		return "", err
	}
	tempPath := file.Name()
	keepTemp := false
	defer func() {
		_ = file.Close()
		if !keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	var bar *mpb.Bar
	var start time.Time
	if pbPrefix != "" {
		start = time.Now()
		total := len(segmentUrls)
		bar = app.pbp.AddBar(int64(total), ProgressBarOptions(pbPrefix)...)
	}

	var written int64
	for _, segmentUrl := range segmentUrls {
		if err := func() error {
			resp, err := app.getMedia(segmentUrl)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return errors.New(resp.Status)
			}
			segBytes, err := readHLSBody(resp.Body, maxHLSSegmentBytes)
			if err != nil {
				return err
			}
			decSegBytes, err := decryptSegment(segBytes, key)
			if err != nil {
				return err
			}
			n, err := file.Write(decSegBytes)
			written += int64(n)
			return err
		}(); err != nil {
			return "", err
		}
		if bar != nil {
			bar.EwmaIncrInt64(1, time.Since(start))
		}
	}
	if written == 0 {
		return "", errors.New("downloaded stream is empty")
	}
	err = file.Close()
	if err != nil {
		return "", err
	}
	if err := app.context().Err(); err != nil {
		return "", err
	}
	keepTemp = true
	return tempPath, nil
}

func remuxToM4A(input, output string) error {
	return remuxToM4AContext(context.Background(), input, output)
}

func remuxToM4AContext(ctx context.Context, input, output string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := securefile.CheckDirectory(filepath.Dir(output)); err != nil {
		return fmt.Errorf("unsafe remux destination: %w", err)
	}
	stagingDirectory, err := os.MkdirTemp(filepath.Dir(output), ".beatportdl-remux-")
	if err != nil {
		return fmt.Errorf("create private remux directory: %w", err)
	}
	defer os.RemoveAll(stagingDirectory)
	tempOutput := filepath.Join(stagingDirectory, "audio.m4a")

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-i", input,
		"-map_metadata", "-1",
		"-c:a", "copy",
		tempOutput,
	)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %w", err)
	}
	info, err := os.Stat(tempOutput)
	if err != nil {
		return fmt.Errorf("stat remuxed m4a: %w", err)
	}
	if info.Size() == 0 {
		return errors.New("remuxed m4a is empty")
	}
	if err := validateM4AContext(ctx, tempOutput); err != nil {
		return fmt.Errorf("validate remuxed m4a: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tempOutput, output); err != nil {
		return fmt.Errorf("move remuxed m4a into place: %w", err)
	}
	return nil
}

func validateM4A(path string) error {
	return validateM4AContext(context.Background(), path)
}

func validateM4AContext(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", path, "-f", "null", "-")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	message := strings.TrimSpace(string(output))
	if err != nil {
		if message == "" {
			return fmt.Errorf("ffmpeg: %w", err)
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, message)
	}
	if message != "" {
		return fmt.Errorf("ffmpeg reported decode errors: %s", message)
	}
	return nil
}
