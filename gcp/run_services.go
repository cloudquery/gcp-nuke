package gcp

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/arehmandev/gcp-nuke/config"
	"github.com/arehmandev/gcp-nuke/helpers"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/syncmap"
	"google.golang.org/api/run/v2"
)

// CloudRunServices -
type CloudRunServices struct {
	serviceClient *run.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	runService, err := run.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	runResource := CloudRunServices{
		serviceClient: runService,
	}

	register(&runResource)
}

// Name - Name of the resourceLister for CloudRunServices
func (c *CloudRunServices) Name() string {
	return "CloudRunServices"
}

// ToSlice - Name of the resourceLister for CloudRunServices
func (c *CloudRunServices) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *CloudRunServices) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all CloudRunServices
func (c *CloudRunServices) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	parent := fmt.Sprintf("projects/%v/locations/-", c.base.config.Project)

	listCall := c.serviceClient.Projects.Locations.Services.List(parent)

	err := listCall.Pages(c.base.config.Ctx, func(page *run.GoogleCloudRunV2ListServicesResponse) error {
		for _, service := range page.Services {
			// a second generation cloud function is itself a run service.
			// Deleting it here would leave the function record behind, so it is
			// left to whatever manages functions
			if service.Labels["goog-managed-by"] == "cloudfunctions" {
				continue
			}

			c.resourceMap.Store(service.Name, nil)
		}

		return nil
	})
	if err != nil {
		// the API is not enabled on every project - do not abort the sweep
		if isAPIUnavailable(err) {
			log.Printf("[Info] Skipping %v - API unavailable on project %v", c.Name(), c.base.config.Project)
			return c.ToSlice()
		}

		log.Fatal(err)
	}

	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *CloudRunServices) Dependencies() []string {
	return []string{}
}

// Remove -
func (c *CloudRunServices) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)

		// Parallel run service deletion
		errs.Go(func() error {
			deleteCall := c.serviceClient.Projects.Locations.Services.Delete(resourceID)

			operation, err := deleteCall.Do()
			if err != nil {
				return err
			}

			done := operation.Done
			seconds := 0

			for !done {
				log.Printf(
					"[Info] Resource currently being deleted %v [type: %v project: %v] (%v seconds)",
					resourceID,
					c.Name(),
					c.base.config.Project,
					seconds,
				)

				operationCall := c.serviceClient.Projects.Locations.Operations.Get(operation.Name)

				checkOpp, err := operationCall.Do()
				if err != nil {
					return err
				}

				if err := cloudRunOperationError(checkOpp); err != nil {
					return err
				}

				done = checkOpp.Done

				time.Sleep(time.Duration(c.base.config.Interval) * time.Second)
				seconds += c.base.config.Interval

				if seconds > c.base.config.Timeout {
					return fmt.Errorf(
						"[Error] Resource deletion timed out for %v [type: %v project: %v] (%v seconds)",
						resourceID,
						c.Name(),
						c.base.config.Project,
						c.base.config.Timeout,
					)
				}
			}

			c.resourceMap.Delete(resourceID)

			log.Printf(
				"[Info] Resource deleted %v [type: %v project: %v] (%v seconds)",
				resourceID,
				c.Name(),
				c.base.config.Project,
				seconds,
			)

			return nil
		})

		return true
	})

	// Wait for all deletions to complete, and return the first non nil error
	err := errs.Wait()
	return err
}
