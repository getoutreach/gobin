// Copyright 2023 Outreach Corporation. All Rights Reserved.

// Description: This file is the entrypoint for the gobin CLI
// command for gobin.
// Managed: true

package main

import (
	"context"

	oapp "github.com/getoutreach/gobox/pkg/app"
	gcli "github.com/getoutreach/gobox/pkg/cli"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	// Place any extra imports for your startup code here
	// <<Stencil::Block(imports)>>
	"github.com/getoutreach/gobin/internal/gobin"
	// <</Stencil::Block>>
)

// HoneycombTracingKey gets set by the Makefile at compile-time which is pulled
// down by devconfig.sh.
var HoneycombTracingKey = "NOTSET" //nolint:gochecknoglobals // Why: We can't compile in things as a const.

// TeleforkAPIKey gets set by the Makefile at compile-time which is pulled
// down by devconfig.sh.
var TeleforkAPIKey = "NOTSET" //nolint:gochecknoglobals // Why: We can't compile in things as a const.

// <<Stencil::Block(honeycombDataset)>>

// HoneycombDataset refers to a custom honeycomb dataset to store traces in, if applicable.
const HoneycombDataset = ""

// <</Stencil::Block>>

// <<Stencil::Block(global)>>

// <</Stencil::Block>>

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	log := logrus.New()

	// <<Stencil::Block(init)>>

	// <</Stencil::Block>>

	app := cli.App{
		Version: oapp.Version,
		Name:    "gobin",
		// <<Stencil::Block(app)>>
		Action: func(c *cli.Context) error {
			return gobin.Run(c.Context, c.Args().First(), c.String("build-dir"), c.String("build-path"), c.Bool("print-path"))
		},
		// <</Stencil::Block>>
	}
	app.Flags = []cli.Flag{
		// <<Stencil::Block(flags)>>
		&cli.BoolFlag{
			Name:    "print-path",
			Aliases: []string{"p"},
		},
		&cli.StringFlag{
			Name:        "build-dir",
			DefaultText: "Manually set the build directory, relative to the root of the repository. Normally this is just the root of the repository.", //nolint:lll //Why: help text
		},
		&cli.StringFlag{
			Name:        "build-path",
			DefaultText: "Manually set the build path, relative to the build directory within the repository. Normally this is just the root of the repository unless overrode with --build-dir.", //nolint:lll //Why: help text
		},
		// <</Stencil::Block>>
	}
	app.Commands = []*cli.Command{
		// <<Stencil::Block(commands)>>

		// <</Stencil::Block>>
	}

	// <<Stencil::Block(postApp)>>

	// <</Stencil::Block>>

	// Insert global flags, tracing, updating and start the application.
	gcli.HookInUrfaveCLI(ctx, cancel, &app, log, HoneycombTracingKey, HoneycombDataset, TeleforkAPIKey)
}
