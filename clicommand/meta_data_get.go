package clicommand

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

var MetaDataGetHelpDescription = `Usage:

   buildkite-agent meta-data get <key> [options...]

Description:

   Get data from a builds key/value store.

Example:

   $ buildkite-agent meta-data get "foo"`

type MetaDataGetConfig struct {
	Key     string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Default string `cli:"default"`
	Job     string `cli:"job" validate:"required"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var MetaDataGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get data from a build",
	Description: MetaDataGetHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "default",
			Value: "",
			Usage: "If the meta-data value doesn't exist return this instead",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job's build should the meta-data be retrieved from",
			EnvVar: "BUILDKITE_JOB_ID",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) {
		ctx := context.Background()

		// The configuration will be loaded into this struct
		cfg := MetaDataGetConfig{}

		loader := cliconfig.Loader{CLI: c, Config: &cfg}
		warnings, err := loader.Load()
		if err != nil {
			fmt.Printf("%s", err)
			os.Exit(1)
		}

		l := CreateLogger(&cfg)

		// Now that we have a logger, log out the warnings that loading config generated
		for _, warning := range warnings {
			l.Warn("%s", warning)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Find the meta data value
		var metaData *api.MetaData
		var resp *api.Response

		err = roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.Constant(5*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			metaData, resp, err = client.GetMetaData(ctx, cfg.Job, cfg.Key)
			// Don't bother retrying if the response was one of these statuses
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 400) {
				r.Break()
				return err
			}
			if err != nil {
				l.Warn("%s (%s)", err, r)
				return err
			}
			return nil
		})

		// Deal with the error if we got one
		if err != nil {
			// Buildkite returns a 404 if the key doesn't exist. If
			// we get this status, and we've got a default - return
			// that instead and bail early.
			//
			// We also use `IsSet` instead of `cfg.Default != ""`
			// to allow people to use a default of a blank string.
			if resp.StatusCode == 404 && c.IsSet("default") {
				l.Warn("No meta-data value exists with key `%s`, returning the supplied default \"%s\"", cfg.Key, cfg.Default)

				fmt.Print(cfg.Default)
				return
			} else {
				l.Fatal("Failed to get meta-data: %s", err)
			}
		}

		// Output the value to STDOUT
		fmt.Print(metaData.Value)
	},
}
