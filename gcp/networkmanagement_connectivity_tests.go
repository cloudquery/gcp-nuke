package gcp

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/arehmandev/gcp-nuke/config"
	"github.com/arehmandev/gcp-nuke/helpers"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/syncmap"
	"google.golang.org/api/networkmanagement/v1"
)

// NetworkManagementConnectivityTests -
type NetworkManagementConnectivityTests struct {
	serviceClient *networkmanagement.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	networkManagementService, err := networkmanagement.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	networkManagementResource := NetworkManagementConnectivityTests{
		serviceClient: networkManagementService,
	}

	register(&networkManagementResource)
}

// Name - Name of the resourceLister for NetworkManagementConnectivityTests
func (c *NetworkManagementConnectivityTests) Name() string {
	return "NetworkManagementConnectivityTests"
}

// ToSlice - Name of the resourceLister for NetworkManagementConnectivityTests
func (c *NetworkManagementConnectivityTests) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *NetworkManagementConnectivityTests) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all NetworkManagementConnectivityTests
func (c *NetworkManagementConnectivityTests) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	parent := fmt.Sprintf("projects/%v/locations/global", c.base.config.Project)

	listCall := c.serviceClient.Projects.Locations.Global.ConnectivityTests.List(parent)

	err := listCall.Pages(c.base.config.Ctx, func(page *networkmanagement.ListConnectivityTestsResponse) error {
		for _, resource := range page.Resources {
			nameSplit := strings.Split(resource.Name, "/")
			c.resourceMap.Store(nameSplit[len(nameSplit)-1], nil)
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
func (c *NetworkManagementConnectivityTests) Dependencies() []string {
	return []string{}
}

// Remove -
func (c *NetworkManagementConnectivityTests) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)

		// Parallel connectivity test deletion
		errs.Go(func() error {
			name := fmt.Sprintf("projects/%v/locations/global/connectivityTests/%v", c.base.config.Project, resourceID)

			deleteCall := c.serviceClient.Projects.Locations.Global.ConnectivityTests.Delete(name)

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

				operationCall := c.serviceClient.Projects.Locations.Global.Operations.Get(operation.Name)

				checkOpp, err := operationCall.Do()
				if err != nil {
					return err
				}

				if err := networkManagementOperationError(checkOpp); err != nil {
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
