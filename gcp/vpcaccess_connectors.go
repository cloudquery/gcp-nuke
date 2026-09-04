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
	"google.golang.org/api/vpcaccess/v1"
)

// VPCAccessConnectors - a serverless VPC access connector owns a subnet, a set
// of firewalls and a managed instance group of e2-micro instances, all of which
// GCP hides from the compute API. The connector is the only handle on them, so
// without this they are undeletable and keep billing for the instances
type VPCAccessConnectors struct {
	serviceClient *vpcaccess.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	vpcAccessService, err := vpcaccess.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	vpcAccessResource := VPCAccessConnectors{
		serviceClient: vpcAccessService,
	}

	register(&vpcAccessResource)
}

// Name - Name of the resourceLister for VPCAccessConnectors
func (c *VPCAccessConnectors) Name() string {
	return "VPCAccessConnectors"
}

// ToSlice - Name of the resourceLister for VPCAccessConnectors
func (c *VPCAccessConnectors) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *VPCAccessConnectors) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all VPCAccessConnectors
func (c *VPCAccessConnectors) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	// the API rejects the "-" location wildcard, so the regions are walked
	for _, region := range c.base.config.Regions {
		parent := fmt.Sprintf("projects/%v/locations/%v", c.base.config.Project, region)

		listCall := c.serviceClient.Projects.Locations.Connectors.List(parent)

		err := listCall.Pages(c.base.config.Ctx, func(page *vpcaccess.ListConnectorsResponse) error {
			for _, connector := range page.Connectors {
				c.resourceMap.Store(connector.Name, nil)
			}

			return nil
		})
		if err != nil {
			if isAPIUnavailable(err) {
				continue
			}

			log.Fatal(err)
		}
	}

	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *VPCAccessConnectors) Dependencies() []string {
	a := CloudRunServices{}

	return []string{a.Name()}
}

// Remove -
func (c *VPCAccessConnectors) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)

		// Parallel connector deletion
		errs.Go(func() error {
			deleteCall := c.serviceClient.Projects.Locations.Connectors.Delete(resourceID)

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

				if err := vpcAccessOperationError(checkOpp); err != nil {
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
