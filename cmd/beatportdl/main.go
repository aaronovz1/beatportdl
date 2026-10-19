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
	"syscall"
	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"
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

	urls                  []string
	activeFiles           map[string]int64
	activeFilesMutex      sync.RWMutex
	readTrackFileIdentity func(path string) (trackFileIdentity, error)
	downloadTrack         func(inst *beatport.Beatport, id int64, quality string) (*beatport.TrackDownload, error)
	streamTrack           func(inst *beatport.Beatport, id int64) (*beatport.TrackStream, error)
	trackIdentityFiles    map[string]*trackIdentityIndex

	bp *beatport.Beatport
}

func main() {
	opts, err := config.ParseCommandLine(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		os.Exit(2)
	}
	cfg, cachePath, err := Setup(opts.Apply)
	if err != nil {
		fmt.Println(err.Error())
		Pause()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	app := &application{
		config:      cfg,
		downloadSem: make(chan struct{}, cfg.MaxDownloadWorkers),
		globalSem:   make(chan struct{}, cfg.MaxGlobalWorkers),
		ctx:         ctx,
		logWriter:   os.Stdout,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		<-sigCh

		if len(app.urls) > 0 {
			app.LogInfo("Shutdown signal received. Cancelling downloads and waiting for workers to stop")
			cancel()

			<-sigCh
		}

		os.Exit(0)
	}()

	if cfg.WriteErrorLog {
		logFilePath, _, err := FindErrorLogFile()
		if err != nil {
			fmt.Println(err.Error())
			Pause()
		}
		f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			panic(err)
		}
		app.logFile = f
		defer f.Close()
	}

	auth := beatport.NewAuth(cfg.Username, cfg.Password, cachePath)
	bp := beatport.New(cfg.Proxy, auth).WithContext(ctx)

	if err := auth.LoadCache(); err != nil {
		if err := auth.Init(bp); err != nil {
			app.FatalError("beatport", err)
		}
	}

	app.bp = bp

	for _, arg := range opts.Args {
		if strings.HasSuffix(arg, ".txt") {
			app.parseTextFile(arg)
		} else {
			app.urls = append(app.urls, arg)
		}
	}

	for {
		if len(app.urls) == 0 {
			app.mainPrompt()
		}

		app.pbp = mpb.New(mpb.WithAutoRefresh(), mpb.WithOutput(color.Output))
		app.logWriter = app.pbp
		app.resetTrackFileState()

		for _, url := range app.urls {
			app.globalWorker(func() {
				app.handleUrl(url)
			})
		}

		app.wg.Wait()
		app.pbp.Shutdown()

		if opts.Quit || ctx.Err() != nil {
			break
		}

		app.urls = []string{}
	}
}
