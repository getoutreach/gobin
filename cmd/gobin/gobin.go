// Copyright 2021 Outreach Corporation. All Rights Reserved.

// Description: This file is the entrypoint for the gobin CLI
// command for gobin.
// Managed: true

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"os/user"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"

	oapp "github.com/getoutreach/gobox/pkg/app"
	"github.com/getoutreach/gobox/pkg/cfg"
	olog "github.com/getoutreach/gobox/pkg/log"
	"github.com/getoutreach/gobox/pkg/secrets"
	"github.com/getoutreach/gobox/pkg/trace"
	"github.com/getoutreach/gobox/pkg/updater"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v3"

	// Place any extra imports for your startup code here
	///Block(imports)
	"github.com/getoutreach/gobin/internal/gobin"
	///EndBlock(imports)
)

// HoneycombTracingKey gets set by the Makefile at compile-time which is pulled
// down by devconfig.sh.
var HoneycombTracingKey = "NOTSET" //nolint:gochecknoglobals // Why: We can't compile in things as a const.

///Block(global)
///EndBlock(global)

// overrideConfigLoaders fakes certain parts of the config that usually get pulled
// in via mechanisms that don't make sense to use in CLIs.
func overrideConfigLoaders() {
	var fallbackSecretLookup func(context.Context, string) ([]byte, error)
	fallbackSecretLookup = secrets.SetDevLookup(func(ctx context.Context, key string) ([]byte, error) {
		if key == "APIKey" {
			return []byte(HoneycombTracingKey), nil
		}

		return fallbackSecretLookup(ctx, key)
	})

	olog.SetOutput(ioutil.Discard)

	fallbackConfigReader := cfg.DefaultReader()
	cfg.SetDefaultReader(func(fileName string) ([]byte, error) {
		if fileName == "trace.yaml" {
			traceConfig := &trace.Config{
				Honeycomb: trace.Honeycomb{
					Enabled: true,
					APIHost: "https://api.honeycomb.io",
					APIKey: cfg.Secret{
						Path: "APIKey",
					},
					///Block(dataset)
					Dataset: "",
					///EndBlock(dataset)
					SamplePercent: 100,
				},
			}
			b, err := yaml.Marshal(&traceConfig)
			if err != nil {
				panic(err)
			}
			return b, nil
		}

		return fallbackConfigReader(fileName)
	})
}

func main() { //nolint:funlen // Why: We can't dwindle this down anymore without adding complexity.
	ctx, cancel := context.WithCancel(context.Background())
	log := logrus.New()

	exitCode := 0
	cli.OsExiter = func(code int) { exitCode = code }

	oapp.SetName("gobin")
	overrideConfigLoaders()

	// handle ^C gracefully
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		out := <-c
		log.Debugf("shutting down: %v", out)
		cancel()
	}()

	if err := trace.InitTracer(ctx, "gobin"); err != nil {
		log.WithError(err).Debugf("failed to start tracer")
	}
	ctx = trace.StartTrace(ctx, "gobin")

	///Block(init)
	///EndBlock(init)

	// optional cleanup function for use after NeedsUpdate
	// this function is re-defined later when NeedsUpdate is true
	cleanup := func() {}

	exit := func() {
		trace.End(ctx)
		trace.CloseTracer(ctx)
		///Block(exit)
		///EndBlock(exit)
		cleanup()
		os.Exit(exitCode)
	}
	defer exit()

	// Print a stack trace when a panic occurs and set the exit code
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("stacktrace from panic: \n" + string(debug.Stack()))

			// Go sets panic exit codes to 2
			exitCode = 2
		}
	}()

	ctx = trace.StartCall(ctx, "main")
	defer trace.EndCall(ctx)

	app := cli.App{
		Version: oapp.Version,
		Name:    "gobin",
		///Block(app)
		Action: func(c *cli.Context) error {
			return gobin.Run(c.Context, c.Args().First(), c.Bool("print-path"))
		},
		///EndBlock(app)
	}
	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:  "skip-update",
			Usage: "skips the updater check",
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "enables debug logging for all components (i.e updater)",
		},
		&cli.BoolFlag{
			Name:  "enable-prereleases",
			Usage: "Enable considering pre-releases when checking for updates",
		},
		&cli.BoolFlag{
			Name:  "force-update-check",
			Usage: "Force checking for an update",
		},
		///Block(flags)
		&cli.BoolFlag{
			Name:    "print-path",
			Aliases: []string{"p"},
		},
		///EndBlock(flags)
	}
	app.Commands = []*cli.Command{
		///Block(commands)
		///EndBlock(commands)
	}

	app.Before = func(c *cli.Context) error {
		///Block(before)
		///EndBlock(before)

		// add info to the root trace about our command and args
		cargs := c.Args().Slice()
		command := ""
		args := make([]string, 0)
		if len(cargs) > 0 {
			command = c.Args().Slice()[0]
		}
		if len(cargs) > 1 {
			args = cargs[1:]
		}

		userName := ""
		if u, err := user.Current(); err == nil {
			userName = u.Username
		}
		trace.AddInfo(ctx, olog.F{
			"gobin.subcommand": command,
			"gobin.args":       strings.Join(args, " "),
			"os.user":          userName,
			"os.name":          runtime.GOOS,
			///Block(trace)
			///EndBlock(trace)
		})

		// restart when updated
		traceCtx := trace.StartCall(c.Context, "updater.NeedsUpdate")
		defer trace.EndCall(traceCtx)

		// restart when updated
		if updater.NeedsUpdate(traceCtx, log, "", oapp.Version, c.Bool("skip-update"), c.Bool("debug"), c.Bool("enable-prereleases"), c.Bool("force-update-check")) {
			// replace running process(execve)
			switch runtime.GOOS {
			case "linux", "darwin":
				cleanup = func() {
					log.Infof("gobin has been updated")
					//nolint:gosec // Why: We're passing in os.Args
					err := syscall.Exec(os.Args[0], os.Args[1:], os.Environ())
					if err != nil {
						log.WithError(err).Error("failed to execute updated binary")
					}
				}
			default:
				log.Infof("gobin has been updated, please re-run your command")
			}

			exitCode = 5
			trace.EndCall(traceCtx)
			exit()
			return nil
		}

		return nil
	}

	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Errorf("failed to run: %v", err)
		//nolint:errcheck // Why: We're attaching the error to the trace.
		trace.SetCallStatus(ctx, err)
		exitCode = 1

		return
	}
}
