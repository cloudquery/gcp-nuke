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

// NetworkServicesGRPCRoutes - service mesh routes hold a reference on the backend services they target
type NetworkServicesGRPCRoutes struct {
	serviceClient *networkservices.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	networkServicesService, err := networkservices.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	networkServicesResource := NetworkServicesGRPCRoutes{
		serviceClient: networkServicesService,
	}

	register(&networkServicesResource)
}

// Name - Name of the resourceLister for NetworkServicesGRPCRoutes
func (c *NetworkServicesGRPCRoutes) Name() string {
	return "NetworkServicesGRPCRoutes"
}

// ToSlice - Name of the resourceLister for NetworkServicesGRPCRoutes
func (c *NetworkServicesGRPCRoutes) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *NetworkServicesGRPCRoutes) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all NetworkServicesGRPCRoutes
func (c *NetworkServicesGRPCRoutes) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	parent := fmt.Sprintf("projects/%v/locations/global", c.base.config.Project)

	listCall := c.serviceClient.Projects.Locations.GrpcRoutes.List(parent)

	err := listCall.Pages(c.base.config.Ctx, func(page *networkservices.ListGrpcRoutesResponse) error {
		for _, resource := range page.GrpcRoutes {
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
func (c *NetworkServicesGRPCRoutes) Dependencies() []string {
	return []string{}
}

// Remove -
func (c *NetworkServicesGRPCRoutes) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)

		// Parallel grpc route deletion
		errs.Go(func() error {
			deleteCall := c.serviceClient.Projects.Locations.GrpcRoutes.Delete(resourceID)

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
