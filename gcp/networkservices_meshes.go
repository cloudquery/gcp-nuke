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
	"google.golang.org/api/networkservices/v1"
)

// NetworkServicesMeshes - a mesh cannot go until the routes attached to it have gone
type NetworkServicesMeshes struct {
	serviceClient *networkservices.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	networkServicesService, err := networkservices.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	networkServicesResource := NetworkServicesMeshes{
		serviceClient: networkServicesService,
	}

	register(&networkServicesResource)
}

// Name - Name of the resourceLister for NetworkServicesMeshes
func (c *NetworkServicesMeshes) Name() string {
	return "NetworkServicesMeshes"
}

// ToSlice - Name of the resourceLister for NetworkServicesMeshes
func (c *NetworkServicesMeshes) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *NetworkServicesMeshes) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all NetworkServicesMeshes
func (c *NetworkServicesMeshes) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	parent := fmt.Sprintf("projects/%v/locations/global", c.base.config.Project)

	listCall := c.serviceClient.Projects.Locations.Meshes.List(parent)

	err := listCall.Pages(c.base.config.Ctx, func(page *networkservices.ListMeshesResponse) error {
		for _, resource := range page.Meshes {
			if isGoogleReservedName(resource.Name) {
				continue
			}

			c.resourceMap.Store(resource.Name, nil)
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
func (c *NetworkServicesMeshes) Dependencies() []string {
	a := NetworkServicesHTTPRoutes{}
	b := NetworkServicesTCPRoutes{}
	cl := NetworkServicesGRPCRoutes{}

	return []string{a.Name(), b.Name(), cl.Name()}
}

// Remove -
func (c *NetworkServicesMeshes) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)

		// Parallel mesh deletion
		errs.Go(func() error {
			deleteCall := c.serviceClient.Projects.Locations.Meshes.Delete(resourceID)

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

				if err := networkServicesOperationError(checkOpp); err != nil {
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
