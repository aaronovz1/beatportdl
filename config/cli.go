package config

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type CommandLineOptions struct {
	Quit               bool
	Args               []string
	overrides          []func(*AppConfig)
	secrets            []*stdinSecret
	passwordOverridden bool
}

type stdinSecret struct {
	flag  string
	value string
}

func (o *CommandLineOptions) Apply(cfg *AppConfig) {
	for _, override := range o.overrides {
		override(cfg)
	}
}

func (o *CommandLineOptions) PasswordOverridden() bool {
	return o.passwordOverridden
}

// ReadSecrets reads one redirected stdin line per secret flag, in argument order.
// Call it before applying the overrides. It does not echo values or input errors.
func (o *CommandLineOptions) ReadSecrets(readLine func() (string, error)) error {
	for _, secret := range o.secrets {
		value, err := readLine()
		if err != nil {
			return fmt.Errorf("--%s: could not read a line from redirected stdin: %w", secret.flag, err)
		}
		if secret.flag == "password-stdin" && value == "" {
			return fmt.Errorf("--password-stdin: password is empty")
		}
		secret.value = value
	}
	return nil
}

func ParseCommandLine(args []string, output io.Writer) (*CommandLineOptions, error) {
	opts := &CommandLineOptions{}
	flags := flag.NewFlagSet("beatportdl", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: beatportdl [options] [URL or text file ...]")
		fmt.Fprintln(output, "Explicit options override the config file for this run. Put options before URLs or files.")
		fmt.Fprintln(output, "Stdin secret options consume one redirected line each, in option order.")
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
	secretStdinFlag := func(name, usage string, set func(*AppConfig, string)) {
		flags.BoolFunc(name, usage, func(raw string) error {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("expected true or false")
			}
			if value {
				secret := &stdinSecret{flag: name}
				opts.secrets = append(opts.secrets, secret)
				opts.overrides = append(opts.overrides, func(cfg *AppConfig) { set(cfg, secret.value) })
				if name == "password-stdin" {
					opts.passwordOverridden = true
				}
			}
			return nil
		})
	}

	stringFlag("username", "Beatport username", func(c *AppConfig, v string) { c.Username = v })
	flags.Func("password", "Beatport password (prefer --password-stdin)", func(value string) error {
		if value != "" {
			fmt.Fprintln(output, "Warning: --password exposes secrets in command arguments and possibly shell history; use --password-stdin instead.")
		}
		opts.passwordOverridden = true
		opts.overrides = append(opts.overrides, func(cfg *AppConfig) { cfg.Password = value })
		return nil
	})
	secretStdinFlag("password-stdin", "Read the Beatport password from redirected stdin", func(c *AppConfig, v string) { c.Password = v })
	stringFlag("quality", "Download quality: medium-hls, medium, high, lossless", func(c *AppConfig, v string) { c.Quality = v })
	boolFlag("write-error-log", "Write errors to the error log", func(c *AppConfig, v bool) { c.WriteErrorLog = v })
	boolFlag("show-progress", "Show download progress bars", func(c *AppConfig, v bool) { c.ShowProgress = v })
	intFlag("max-global-workers", "Concurrent URL jobs (positive integer)", func(c *AppConfig, v int) { c.MaxGlobalWorkers = v })
	intFlag("max-download-workers", "Concurrent downloads (positive integer)", func(c *AppConfig, v int) { c.MaxDownloadWorkers = v })
	stringFlag("downloads-directory", "Download destination", func(c *AppConfig, v string) { c.DownloadsDirectory = v })
	boolFlag("sort-by-context", "Organize downloads by context", func(c *AppConfig, v bool) { c.SortByContext = v })
	boolFlag("sort-by-label", "Use label names as parent release directories", func(c *AppConfig, v bool) { c.SortByLabel = v })
	boolFlag("force-release-directories", "Create release directories within playlists, charts, artists, and labels", func(c *AppConfig, v bool) { c.ForceReleaseDirectories = v })
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
	flags.Func("proxy", "Proxy URL (use --proxy-stdin for credentials)", func(value string) error {
		if strings.Contains(value, "@") {
			fmt.Fprintln(output, "Warning: --proxy can expose credentials in command arguments and possibly shell history; use --proxy-stdin instead.")
		}
		opts.overrides = append(opts.overrides, func(cfg *AppConfig) { cfg.Proxy = value })
		return nil
	})
	secretStdinFlag("proxy-stdin", "Read the proxy URL from redirected stdin", func(c *AppConfig, v string) { c.Proxy = v })
	flags.Func("tag-mappings", "Tag mappings as a YAML or JSON map (replaces configured mappings)", func(raw string) error {
		var mappings map[string]map[string]string
		decoder := yaml.NewDecoder(strings.NewReader(raw))
		decoder.KnownFields(true)
		if err := decoder.Decode(&mappings); err != nil {
			return fmt.Errorf("expected a YAML or JSON map of formats to tag mappings")
		}
		if err := decoder.Decode(new(interface{})); err != io.EOF {
			return fmt.Errorf("expected exactly one tag mappings document")
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
