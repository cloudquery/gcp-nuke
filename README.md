# gcp-Nuke

## Description

Destroy GCP resource within a specified project

## Usage

```shell
NAME:
   gcp-nuke - The GCP project cleanup tool with added radiation

USAGE:
   e.g. gcp-nuke --project test-nuke-123456 --dryrun

VERSION:
   v0.1.0

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --project value               GCP project id to nuke (required)
   --dryrun                      Perform a dryrun instead (default: false)
   --timeout value               Timeout for removal of a single resource
seconds (default: 400)
   --interval value              Interval in seconds for polling resource
deletion status (default: 10)
   --skip-gke-autopilot-clusters Skip processing of GKE Autopilot clusters if
found. gcp-nuke will error if any exist. (default: false)
   --help, -h                    show help (default: false)
   --version, -v                 print the version (default: false)
```
