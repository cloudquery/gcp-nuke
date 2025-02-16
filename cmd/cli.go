package cmd

import (
	"log"
	"os"

	"github.com/arehmandev/gcp-nuke/config"
	"github.com/arehmandev/gcp-nuke/gcp"
	"github.com/urfave/cli/v2"
)

// Command -
func Command() {
	app := &cli.App{
		Usage:     "The GCP project cleanup tool with added radiation",
		Version:   "v0.1.0",
		UsageText: "e.g. gcp-nuke --project test-nuke-262510 --dryrun",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "project, p",
				Usage:    "GCP project id to nuke (required)",
				Required: true,
			},
			&cli.BoolFlag{
				Name:  "dryrun, d",
				Usage: "Perform a dryrun instead",
			},
			&cli.IntFlag{
				Name:  "timeout, t",
				Value: 400,
				Usage: "Timeout for removal of a single resource in seconds",
			},
			&cli.IntFlag{
				Name:  "interval, i",
				Value: 10,
				Usage: "Interval in seconds for polling resource deletion status",
			},
			&cli.BoolFlag{
				Name:  "skip-gke-autopilot-clusters, s",
				Usage: "Skip GKE Autopilot clusters",
				Value: false,
			},
		},
		Action: func(c *cli.Context) error {
			// Behaviour to delete all resource in parallel in one project at a time - will be made into loop / concurrenct project nuke if required
			config := config.Config{
				Project:                  c.String("project"),
				DryRun:                   c.Bool("dryrun"),
				Timeout:                  c.Int("timeout"),
				Interval:                 c.Int("interval"),
				Ctx:                      gcp.Ctx,
				Zones:                    gcp.GetZones(gcp.Ctx, c.String("project")),
				Regions:                  gcp.GetRegions(gcp.Ctx, c.String("project")),
				SkipGKEAutopilotClusters: c.Bool("skip-gke-autopilot-clusters"),
			}

			log.Printf("[Info] Timeout %v seconds. Polltime %v seconds. Dry run: %v", config.Timeout, config.Interval, config.DryRun)

			gcp.RemoveProject(config)

			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
