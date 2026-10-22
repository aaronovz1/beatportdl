package config

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"unspok3n/beatportdl/internal/securefile"
	"unspok3n/beatportdl/internal/validator"

	"go.yaml.in/yaml/v3"
)

type AppConfig struct {
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	Quality       string `yaml:"quality,omitempty"`
	WriteErrorLog bool   `yaml:"write_error_log"`
	ShowProgress  bool   `yaml:"show_progress"`

	// Zero workers and empty required enums retain partial-config construction;
	// all valid false, zero, and empty settings must be emitted for round trips.
	MaxGlobalWorkers   int `yaml:"max_global_workers,omitempty"`
	MaxDownloadWorkers int `yaml:"max_download_workers,omitempty"`

	DownloadsDirectory      string `yaml:"downloads_directory"`
	SortByContext           bool   `yaml:"sort_by_context"`
	SortByLabel             bool   `yaml:"sort_by_label"`
	ForceReleaseDirectories bool   `yaml:"force_release_directories"`
	TrackExists             string `yaml:"track_exists,omitempty"`
	TrackNumberPadding      int    `yaml:"track_number_padding"`

	ReleaseDirectoryTemplate  string `yaml:"release_directory_template"`
	PlaylistDirectoryTemplate string `yaml:"playlist_directory_template"`
	ChartDirectoryTemplate    string `yaml:"chart_directory_template"`
	LabelDirectoryTemplate    string `yaml:"label_directory_template"`
	ArtistDirectoryTemplate   string `yaml:"artist_directory_template"`
	TrackFileTemplate         string `yaml:"track_file_template"`
	WhitespaceCharacter       string `yaml:"whitespace_character"`
	ArtistsLimit              int    `yaml:"artists_limit"`
	ArtistsShortForm          string `yaml:"artists_short_form"`
	KeySystem                 string `yaml:"key_system,omitempty"`

	CoverSize string `yaml:"cover_size"`
	KeepCover bool   `yaml:"keep_cover"`
	FixTags   bool   `yaml:"fix_tags"`

	TagMappings map[string]map[string]string `yaml:"tag_mappings"`

	Proxy string `yaml:"proxy"`
}

const (
	DefaultCoverSize = "1400x1400"
)

var (
	SupportedTrackExistsOptions = []string{
		"error",
		"skip",
		"overwrite",
		"update",
	}

	SupportedKeySystems = []string{
		"standard",
		"standard-short",
		"openkey",
		"camelot",
	}
)

func FFMPEGInstalled() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// DefaultConfig supplies the same defaults for first-run setup and file parsing.
func DefaultConfig() AppConfig {
	return AppConfig{
		Quality:                   "lossless",
		CoverSize:                 DefaultCoverSize,
		TrackFileTemplate:         "{number}. {artists} - {name} ({mix_name})",
		ReleaseDirectoryTemplate:  "[{catalog_number}] {artists} - {name}",
		PlaylistDirectoryTemplate: "{name} [{created_date}]",
		ChartDirectoryTemplate:    "{name} [{published_date}]",
		LabelDirectoryTemplate:    "{name} [{updated_date}]",
		ArtistDirectoryTemplate:   "{name}",
		ArtistsLimit:              3,
		ArtistsShortForm:          "VA",
		KeySystem:                 "standard-short",
		TrackExists:               "update",
		TrackNumberPadding:        2,
		FixTags:                   true,
		ShowProgress:              true,
		MaxGlobalWorkers:          15,
		MaxDownloadWorkers:        15,
	}
}

func Parse(filePath string, overrides ...func(*AppConfig)) (*AppConfig, error) {
	data, err := securefile.Read(filePath)
	if err != nil {
		return nil, err
	}
	config := DefaultConfig()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config file: %w", err)
	}
	for _, override := range overrides {
		override(&config)
	}

	if config.Username == "" || config.Password == "" {
		return nil, fmt.Errorf("username or password is not provided")
	}

	if config.Quality == "medium-hls" && !FFMPEGInstalled() {
		return nil, errors.New("ffmpeg not found")
	}

	if config.TagMappings != nil {
		if err = ValidateTagMappings(config.TagMappings); err != nil {
			return nil, err
		}

		if _, ok := config.TagMappings["flac"]; !ok {
			config.TagMappings["flac"] = DefaultTagMappings["flac"]
		}

		if _, ok := config.TagMappings["m4a"]; !ok {
			config.TagMappings["m4a"] = DefaultTagMappings["m4a"]
		}
	} else {
		config.TagMappings = DefaultTagMappings
	}

	if !validator.PermittedValue(config.KeySystem, SupportedKeySystems...) {
		return nil, fmt.Errorf("invalid key system")
	}

	if config.DownloadsDirectory == "" {
		return nil, fmt.Errorf("no downloads directory provided")
	}

	if !validator.PermittedValue(config.TrackExists, SupportedTrackExistsOptions...) {
		return nil, fmt.Errorf("invalid track exists behavior")
	}

	if config.TrackNumberPadding > 10 || config.TrackNumberPadding < 0 {
		return nil, fmt.Errorf("invalid track number padding")
	}
	if config.MaxGlobalWorkers <= 0 || config.MaxDownloadWorkers <= 0 {
		return nil, fmt.Errorf("worker counts must be positive")
	}
	if !validator.SafePathReplacement(config.WhitespaceCharacter) {
		return nil, fmt.Errorf("whitespace_character must not contain path separators, reserved filename characters, or control characters")
	}

	return &config, nil
}

func (c *AppConfig) Save(filePath string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := securefile.Write(filePath, data); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
