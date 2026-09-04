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
	"google.golang.org/api/compute/v1"
)

// ComputeZoneNetworkEndpointGroups -
type ComputeZoneNetworkEndpointGroups struct {
	serviceClient *compute.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	computeService, err := compute.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	computeResource := ComputeZoneNetworkEndpointGroups{
		serviceClient: computeService,
	}

	register(&computeResource)
}

// Name - Name of the resourceLister for ComputeZoneNetworkEndpointGroups
func (c *ComputeZoneNetworkEndpointGroups) Name() string {
	return "ComputeZoneNetworkEndpointGroups"
}

// ToSlice - Name of the resourceLister for ComputeZoneNetworkEndpointGroups
func (c *ComputeZoneNetworkEndpointGroups) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *ComputeZoneNetworkEndpointGroups) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all ComputeZoneNetworkEndpointGroups
func (c *ComputeZoneNetworkEndpointGroups) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	for _, zone := range c.base.config.Zones {
		listCall := c.serviceClient.NetworkEndpointGroups.List(c.base.config.Project, zone)

		resourceList, err := listCall.Do()
		if err != nil {
			log.Fatal(err)
		}

		for _, resource := range resourceList.Items {
			// the same name exists in every zone GKE uses, so key on both
			c.resourceMap.Store(fmt.Sprintf("%v/%v", zone, resource.Name), zone)
		}
	}

	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *ComputeZoneNetworkEndpointGroups) Dependencies() []string {
	a := ComputeBackendServices{}

	return []string{a.Name()}
}

// Remove -
func (c *ComputeZoneNetworkEndpointGroups) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceKey := key.(string)
		zone := value.(string)
		resourceID := strings.TrimPrefix(resourceKey, zone+"/")

		// Parallel network endpoint group deletion
		errs.Go(func() error {
			deleteCall := c.serviceClient.NetworkEndpointGroups.Delete(c.base.config.Project, zone, resourceID)

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

				operationCall := c.serviceClient.ZoneOperations.Get(c.base.config.Project, zone, operation.Name)

				checkOpp, err := operationCall.Do()
				if err != nil {
					return err
				}

				if err := computeOperationError(checkOpp); err != nil {
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

			c.resourceMap.Delete(resourceKey)

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
