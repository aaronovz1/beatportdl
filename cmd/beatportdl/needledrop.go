package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/grafov/m3u8"
	"github.com/vbauerster/mpb/v8"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type StreamKey struct {
	Value []byte
	IV    []byte
}

func getStreamSegments(stream string) (*[]string, *StreamKey, error) {
	resp, err := http.Get(stream)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}
	playlist, _, err := m3u8.DecodeFrom(resp.Body, true)
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
	base := u.Scheme + "://" + u.Host + path.Dir(u.Path) + "/"
	var segments []string
	var streamKey StreamKey
	for i, segment := range media.Segments {
		if segment == nil {
			break
		}
		if segment.URI == "" {
			return nil, nil, fmt.Errorf("stream segment %d has empty URI", i)
		}
		if i == 0 {
			if segment.Key == nil || segment.Key.URI == "" {
				return nil, nil, fmt.Errorf("stream segment %d is missing an encryption key URI", i)
			}
			if segment.Key.IV == "" {
				return nil, nil, fmt.Errorf("stream segment %d is missing an encryption key IV", i)
			}
			req, err := http.Get(base + segment.Key.URI)
			if err != nil {
				return nil, nil, err
			}
			defer req.Body.Close()
			if req.StatusCode != http.StatusOK {
				return nil, nil, fmt.Errorf("get stream key failed with status code: %d", req.StatusCode)
			}
			keyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("read stream key: %v", err)
			}
			ivBytes, err := hex.DecodeString(strings.TrimPrefix(segment.Key.IV, "0x"))
			if err != nil {
				return nil, nil, fmt.Errorf("decode stream iv: %v", err)
			}
			streamKey.Value = keyBytes
			streamKey.IV = ivBytes
		}
		segments = append(segments, base+segment.URI)
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
			resp, err := http.Get(segmentUrl)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return errors.New(resp.Status)
			}
			segBytes, err := io.ReadAll(resp.Body)
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
	keepTemp = true
	return tempPath, nil
}

func remuxToM4A(input, output string) error {
	tempFile, err := os.CreateTemp(filepath.Dir(output), ".beatportdl-remux-*.m4a")
	if err != nil {
		return fmt.Errorf("create temp output: %w", err)
	}
	tempOutput := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempOutput)
		return fmt.Errorf("close temp output: %w", err)
	}
	_ = os.Remove(tempOutput)
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tempOutput)
		}
	}()

	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", input,
		"-map_metadata", "-1",
		"-c:a", "copy",
		tempOutput,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	info, err := os.Stat(tempOutput)
	if err != nil {
		return fmt.Errorf("stat remuxed m4a: %w", err)
	}
	if info.Size() == 0 {
		return errors.New("remuxed m4a is empty")
	}
	if err := validateM4A(tempOutput); err != nil {
		return fmt.Errorf("validate remuxed m4a: %w", err)
	}
	if err := os.Rename(tempOutput, output); err != nil {
		return fmt.Errorf("move remuxed m4a into place: %w", err)
	}
	keepTemp = true
	return nil
}

func validateM4A(path string) error {
	cmd := exec.Command("ffmpeg", "-v", "error", "-i", path, "-f", "null", "-")
	output, err := cmd.CombinedOutput()
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
