package config

import (
	"context"
)

// Config -
type Config struct {
	Ctx                      context.Context
	Project                  string
	Zones                    []string
	Regions                  []string
	Timeout                  int
	PollTime                 int
	DryRun                   bool
	SkipGKEAutopilotClusters bool
}
