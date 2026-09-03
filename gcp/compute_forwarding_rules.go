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
	"google.golang.org/api/compute/v1"
)

// ComputeForwardingRules -
type ComputeForwardingRules struct {
	serviceClient *compute.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	computeService, err := compute.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	computeResource := ComputeForwardingRules{
		serviceClient: computeService,
	}

	register(&computeResource)
}

// Name - Name of the resourceLister for ComputeForwardingRules
func (c *ComputeForwardingRules) Name() string {
	return "ComputeForwardingRules"
}

// ToSlice - Name of the resourceLister for ComputeForwardingRules
func (c *ComputeForwardingRules) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *ComputeForwardingRules) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all ComputeForwardingRules
func (c *ComputeForwardingRules) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	for _, region := range c.base.config.Regions {
		listCall := c.serviceClient.ForwardingRules.List(c.base.config.Project, region)

		resourceList, err := listCall.Do()
		if err != nil {
			log.Fatal(err)
		}

		for _, resource := range resourceList.Items {
			c.resourceMap.Store(resource.Name, region)
		}
	}

	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *ComputeForwardingRules) Dependencies() []string {
	a := ContainerGKEClusters{}

	return []string{a.Name()}
}

// Remove -
func (c *ComputeForwardingRules) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)
		region := value.(string)

		// Parallel forwarding rule deletion
		errs.Go(func() error {
			deleteCall := c.serviceClient.ForwardingRules.Delete(c.base.config.Project, region, resourceID)

			operation, err := deleteCall.Do()
			if err != nil {
				return err
			}

			opStatus := ""
			seconds := 0

			for opStatus != "DONE" {
				log.Printf(
					"[Info] Resource currently being deleted %v [type: %v project: %v] (%v seconds)",
					resourceID,
					c.Name(),
					c.base.config.Project,
					seconds,
				)

				operationCall := c.serviceClient.RegionOperations.Get(c.base.config.Project, region, operation.Name)

				checkOpp, err := operationCall.Do()
				if err != nil {
					return err
				}

				opStatus = checkOpp.Status

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
