package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"
)

func Setup(overrides ...func(*config.AppConfig)) (cfg *config.AppConfig, cachePath string, err error) {
	return setupConfig(newCLIInput(context.Background(), os.Stdin), false, overrides...)
}

func setupConfig(input *cliInput, passwordOverridden bool, overrides ...func(*config.AppConfig)) (*config.AppConfig, string, error) {
	if err := input.ctx.Err(); err != nil {
		return nil, "", err
	}
	configFilePath, exists, err := FindConfigFile()
	if err != nil {
		return nil, "", err
	}

	if !exists {
		fmt.Println("Config file not found, creating a new one:", presentationText(configFilePath))

		fmt.Print("Username: ")
		username, err := input.line()
		if err != nil {
			return nil, configFilePath, fmt.Errorf("read username: %w", err)
		}
		var password string
		if !passwordOverridden {
			fmt.Print("Password: ")
			password, err = input.password()
			if err != nil {
				return nil, configFilePath, fmt.Errorf("read password: %w", err)
			}
		}
		fmt.Print("Downloads directory: ")
		downloadsDir, err := input.line()
		if err != nil {
			return nil, configFilePath, fmt.Errorf("read downloads directory: %w", err)
		}

		cfg := config.DefaultConfig()
		cfg.Username = username
		cfg.Password = password
		cfg.DownloadsDirectory = downloadsDir

		fmt.Println("1. Lossless (44.1 khz FLAC)\n2. High (256 kbps AAC)\n3. Medium (128 kbps AAC)\n4. Medium HLS (128 kbps AAC)")
		for {
			fmt.Print("Quality: ")
			qualityNumber, err := input.line()
			if err != nil {
				return nil, configFilePath, fmt.Errorf("read quality: %w", err)
			}
			switch qualityNumber {
			case "1":
				cfg.Quality = "lossless"
			case "2":
				cfg.Quality = "high"
			case "3":
				cfg.Quality = "medium"
			case "4":
				cfg.Quality = "medium-hls"
			default:
				fmt.Println("Invalid quality")
				continue
			}
			break
		}

		if err := input.ctx.Err(); err != nil {
			return nil, configFilePath, err
		}
		if err := cfg.Save(configFilePath); err != nil {
			return nil, configFilePath, fmt.Errorf("save config: %w", err)
		}
	}

	parsedConfig, err := config.Parse(configFilePath, overrides...)
	if err != nil {
		return nil, configFilePath, fmt.Errorf("load config: %w", err)
	}

	cacheFilePath, exists, err := FindCacheFile()
	if err != nil {
		return nil, configFilePath, fmt.Errorf("get executable path: %w", err)
	}

	return parsedConfig, cacheFilePath, nil
}

func (app *application) mainPrompt(reader *cliInput) error {
	fmt.Print("Enter url or search query: ")
	input, err := reader.line()
	if err != nil {
		return err
	}
	if lower := strings.ToLower(input); strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		app.urls = append(app.urls, input)
		return nil
	}
	if strings.TrimSpace(input) == "" {
		return nil
	}
	return app.search(input, reader)
}

func (app *application) search(input string, reader *cliInput) error {
	query, err := beatportSearchQuery(input)
	if err != nil {
		app.LogError("search", err)
		return nil
	}
	results, err := app.bp.Search(query)
	if err != nil {
		return fmt.Errorf("search Beatport: %w", err)
	}
	trackResultsLen := len(results.Tracks)
	releasesResultsLen := len(results.Releases)

	if trackResultsLen+releasesResultsLen == 0 {
		fmt.Println("No results found")
		return nil
	}

	fmt.Println("Search results:")
	fmt.Println("[ Tracks ]")
	for i, track := range results.Tracks {
		app.writeLog(presentationText(fmt.Sprintf(
			"%2d. %s - %s (%s) [%s]", i+1,
			track.Artists.Display(
				app.config.ArtistsLimit,
				app.config.ArtistsShortForm,
			),
			track.Name.String(),
			track.MixName.String(),
			track.Length,
		)) + "\n")
	}
	fmt.Println("\n[ Releases ]")
	indexOffset := trackResultsLen + 1
	for i, release := range results.Releases {
		app.writeLog(presentationText(fmt.Sprintf(
			"%2d. %s - %s [%s]", i+indexOffset,
			release.Artists.Display(
				app.config.ArtistsLimit,
				app.config.ArtistsShortForm,
			),
			release.Name.String(),
			release.Label.Name,
		)) + "\n")
	}
	fmt.Print("Enter the result number(s): ")
	input, err = reader.line()
	if err != nil {
		return err
	}
	requestedResults := strings.Fields(input)
	for _, result := range requestedResults {
		resultInt, err := strconv.Atoi(result)
		if err != nil {
			fmt.Printf("invalid result number: %s\n", presentationText(result))
			continue
		}

		if resultInt > releasesResultsLen+trackResultsLen || resultInt < 1 {
			fmt.Printf("invalid result number: %d\n", resultInt)
			continue
		}

		if resultInt >= indexOffset {
			app.urls = append(app.urls, results.Releases[resultInt-indexOffset].URL)
		} else {
			app.urls = append(app.urls, results.Tracks[resultInt-1].URL)
		}
	}
	return nil
}

func beatportSearchQuery(input string) (string, error) {
	var terms []string
	for _, term := range strings.Fields(input) {
		switch strings.ToLower(term) {
		case "@beatsource":
			return "", beatport.ErrBeatsourceRemoved
		case "@beatport":
			continue
		default:
			terms = append(terms, term)
		}
	}

	return strings.Join(terms, " "), nil
}

func (app *application) parseTextFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	firstURL := len(app.urls)

	for scanner.Scan() {
		if err := app.context().Err(); err != nil {
			return err
		}
		app.urls = append(app.urls, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s at line %d: %w", path, len(app.urls)-firstURL+1, err)
	}
	return nil
}

var (
	ErrUnsupportedLinkType = errors.New("unsupported link type")
)
