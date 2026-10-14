package config

import (
	"flag"
	"fmt"
	"io"
	"strconv"

	"gopkg.in/yaml.v2"
)

type CommandLineOptions struct {
	Quit      bool
	Args      []string
	overrides []func(*AppConfig)
}

func (o *CommandLineOptions) Apply(cfg *AppConfig) {
	for _, override := range o.overrides {
		override(cfg)
	}
}

func ParseCommandLine(args []string, output io.Writer) (*CommandLineOptions, error) {
	opts := &CommandLineOptions{}
	flags := flag.NewFlagSet("beatportdl", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: beatportdl [options] [URL or text file ...]")
		fmt.Fprintln(output, "Explicit options override the config file for this run. Put options before URLs or files.")
		flags.PrintDefaults()
	}
	flags.BoolVar(&opts.Quit, "q", false, "Quit the main loop after finishing")

	stringFlag := func(name, usage string, set func(*AppConfig, string)) {
		flags.Func(name, usage, func(value string) error {
			opts.overrides = append(opts.overrides, func(cfg *AppConfig) { set(cfg, value) })
			return nil
		})
	}
	boolFlag := func(name, usage string, set func(*AppConfig, bool)) {
		flags.BoolFunc(name, usage, func(raw string) error {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("expected true or false")
			}
			opts.overrides = append(opts.overrides, func(cfg *AppConfig) { set(cfg, value) })
			return nil
		})
	}
	intFlag := func(name, usage string, set func(*AppConfig, int)) {
		flags.Func(name, usage, func(raw string) error {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("expected an integer")
			}
			opts.overrides = append(opts.overrides, func(cfg *AppConfig) { set(cfg, value) })
			return nil
		})
	}

	stringFlag("username", "Beatport username", func(c *AppConfig, v string) { c.Username = v })
	stringFlag("password", "Beatport password", func(c *AppConfig, v string) { c.Password = v })
	stringFlag("quality", "Download quality: medium-hls, medium, high, lossless", func(c *AppConfig, v string) { c.Quality = v })
	boolFlag("write-error-log", "Write errors to the error log", func(c *AppConfig, v bool) { c.WriteErrorLog = v })
	boolFlag("show-progress", "Show download progress bars", func(c *AppConfig, v bool) { c.ShowProgress = v })
	intFlag("max-global-workers", "Concurrent URL jobs (positive integer)", func(c *AppConfig, v int) { c.MaxGlobalWorkers = v })
	intFlag("max-download-workers", "Concurrent downloads (positive integer)", func(c *AppConfig, v int) { c.MaxDownloadWorkers = v })
	stringFlag("downloads-directory", "Download destination", func(c *AppConfig, v string) { c.DownloadsDirectory = v })
	boolFlag("sort-by-context", "Organize downloads by context", func(c *AppConfig, v bool) { c.SortByContext = v })
	boolFlag("sort-by-label", "Use label names as parent release directories", func(c *AppConfig, v bool) { c.SortByLabel = v })
	boolFlag("force-release-directories", "Create release directories within playlists and charts", func(c *AppConfig, v bool) { c.ForceReleaseDirectories = v })
	stringFlag("track-exists", "Existing track behavior: error, skip, overwrite, update", func(c *AppConfig, v string) { c.TrackExists = v })
	intFlag("track-number-padding", "Track number padding (0-10; 0 uses track count)", func(c *AppConfig, v int) { c.TrackNumberPadding = v })
	stringFlag("release-directory-template", "Release directory template", func(c *AppConfig, v string) { c.ReleaseDirectoryTemplate = v })
	stringFlag("playlist-directory-template", "Playlist directory template", func(c *AppConfig, v string) { c.PlaylistDirectoryTemplate = v })
	stringFlag("chart-directory-template", "Chart directory template", func(c *AppConfig, v string) { c.ChartDirectoryTemplate = v })
	stringFlag("label-directory-template", "Label directory template", func(c *AppConfig, v string) { c.LabelDirectoryTemplate = v })
	stringFlag("artist-directory-template", "Artist directory template", func(c *AppConfig, v string) { c.ArtistDirectoryTemplate = v })
	stringFlag("track-file-template", "Track filename template", func(c *AppConfig, v string) { c.TrackFileTemplate = v })
	stringFlag("whitespace-character", "Replacement for spaces in filenames and directories", func(c *AppConfig, v string) { c.WhitespaceCharacter = v })
	intFlag("artists-limit", "Artist count before abbreviation", func(c *AppConfig, v int) { c.ArtistsLimit = v })
	stringFlag("artists-short-form", "Artist abbreviation", func(c *AppConfig, v string) { c.ArtistsShortForm = v })
	stringFlag("key-system", "Key system: standard, standard-short, openkey, camelot", func(c *AppConfig, v string) { c.KeySystem = v })
	stringFlag("cover-size", "Cover art size", func(c *AppConfig, v string) { c.CoverSize = v })
	boolFlag("keep-cover", "Keep cover.jpg in context directories", func(c *AppConfig, v bool) { c.KeepCover = v })
	boolFlag("fix-tags", "Write audio metadata tags", func(c *AppConfig, v bool) { c.FixTags = v })
	stringFlag("proxy", "Proxy URL", func(c *AppConfig, v string) { c.Proxy = v })
	flags.Func("tag-mappings", "Tag mappings as a YAML or JSON map (replaces configured mappings)", func(raw string) error {
		var mappings map[string]map[string]string
		if err := yaml.UnmarshalStrict([]byte(raw), &mappings); err != nil {
			return fmt.Errorf("expected a YAML or JSON map of formats to tag mappings")
		}
		opts.overrides = append(opts.overrides, func(cfg *AppConfig) { cfg.TagMappings = mappings })
		return nil
	})

	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	opts.Args = flags.Args()
	return opts, nil
}
