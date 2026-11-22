package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/fatih/color"
	"github.com/vbauerster/mpb/v8"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"
	"unspok3n/beatportdl/internal/securefile"
)

const (
	configFilename = "beatportdl-config.yml"
	cacheFilename  = "beatportdl-credentials.json"
	errorFilename  = "beatportdl-err.log"
)

type application struct {
	config      *config.AppConfig
	logFile     *os.File
	logWriter   io.Writer
	ctx         context.Context
	wg          sync.WaitGroup
	downloadSem chan struct{}
	globalSem   chan struct{}
	pbp         *mpb.Progress
	mediaClient *http.Client

	directoryCleanupMutex sync.Mutex
	activeURLWorkers      int
	pendingDirectories    map[string]struct{}

	urls                  []string
	trackFilesMutex       sync.Mutex
	activeFiles           map[string]int64
	trackDirectories      map[string]*trackDirectoryState
	readTrackFileIdentity func(path string) (trackFileIdentity, error)
	downloadTrack         func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error)
	streamTrack           func(inst *beatport.Beatport, id int64) (*beatport.TrackStream, error)

	bp *beatport.Beatport

	failures atomic.Uint64
}

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runCLI(ctx, os.Args[1:], os.Stdin)
}

func runCLI(ctx context.Context, args []string, stdin *os.File) (status int) {
	defer func() {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "Shutdown signal received; interrupted")
			status = 130
		}
	}()

	opts, err := config.ParseCommandLine(args, os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	input := newCLIInput(ctx, stdin)
	if err := opts.ReadSecrets(input.secretLine); err != nil {
		return reportCLIError(ctx, err)
	}
	cfg, cachePath, err := setupConfig(input, opts.PasswordOverridden(), opts.Apply)
	if err != nil {
		return reportCLIError(ctx, err)
	}

	proxyURL, err := cfg.ProxyURL()
	if err != nil {
		return reportCLIError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return reportCLIError(ctx, err)
	}

	app := &application{
		config:      cfg,
		downloadSem: make(chan struct{}, cfg.MaxDownloadWorkers),
		globalSem:   make(chan struct{}, cfg.MaxGlobalWorkers),
		ctx:         ctx,
		logWriter:   os.Stdout,
		mediaClient: newMediaClient(proxyURL),
	}
	defer app.mediaClient.CloseIdleConnections()

	if cfg.WriteErrorLog {
		logFilePath, _, err := FindErrorLogFile()
		if err != nil {
			return reportCLIError(ctx, fmt.Errorf("find error log: %w", err))
		}
		f, err := securefile.OpenAppend(logFilePath)
		if err != nil {
			return reportCLIError(ctx, fmt.Errorf("open error log: %w", err))
		}
		app.logFile = f
		defer f.Close()
	}

	auth := beatport.NewAuth(cfg.Username, cfg.Password, cachePath)
	bp := beatport.New(proxyURL, auth).WithContext(ctx)
	app.bp = bp

	if err := auth.LoadCache(); err != nil {
		if err := auth.Init(bp); err != nil {
			if ctx.Err() == nil {
				app.LogError("beatport", err)
			}
			return 1
		}
	}

	if err := app.runJobs(input, opts.Args, opts.Quit, app.handleUrl); err != nil {
		return reportCLIError(ctx, err)
	}
	return 0
}

func reportCLIError(ctx context.Context, err error) int {
	if ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, presentationText(err.Error()))
	}
	return 1
}

func (app *application) runJobs(input *cliInput, args []string, quit bool, handleURL func(string)) error {
	ctx := app.context()
	for _, arg := range args {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasSuffix(arg, ".txt") {
			if err := app.parseTextFile(arg); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				app.LogError("read input text file", err)
			}
		} else {
			app.urls = append(app.urls, arg)
		}
	}

	for batch := len(args) != 0; ctx.Err() == nil; batch = false {
		if !batch {
			if err := app.mainPrompt(input); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				if ctx.Err() == nil {
					app.LogError("prompt", err)
				}
				break
			}
		} else if len(app.urls) == 0 {
			break
		}

		if len(app.urls) != 0 && ctx.Err() == nil {
			app.runURLBatch(handleURL)
		}
		if quit {
			break
		}
		app.urls = nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if failures := app.failures.Load(); failures != 0 {
		return fmt.Errorf("run finished with %d reported error(s)", failures)
	}
	return nil
}

func (app *application) runURLBatch(handleURL func(string)) {
	writer := app.logWriter
	app.pbp = mpb.New(mpb.WithAutoRefresh(), mpb.WithOutput(color.Output))
	app.logWriter = app.pbp
	app.resetTrackFileState()

	for _, url := range app.urls {
		if app.context().Err() != nil {
			break
		}
		app.globalWorker(func() { handleURL(url) })
	}

	app.wg.Wait()
	app.pbp.Shutdown()
	app.logWriter = writer
	app.pbp = nil
}
