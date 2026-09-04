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

// ComputeTargetHTTPSProxies -
type ComputeTargetHTTPSProxies struct {
	serviceClient *compute.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	computeService, err := compute.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	computeResource := ComputeTargetHTTPSProxies{
		serviceClient: computeService,
	}

	register(&computeResource)
}

// Name - Name of the resourceLister for ComputeTargetHTTPSProxies
func (c *ComputeTargetHTTPSProxies) Name() string {
	return "ComputeTargetHTTPSProxies"
}

// ToSlice - Name of the resourceLister for ComputeTargetHTTPSProxies
func (c *ComputeTargetHTTPSProxies) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *ComputeTargetHTTPSProxies) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all ComputeTargetHTTPSProxies
func (c *ComputeTargetHTTPSProxies) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	listCall := c.serviceClient.TargetHttpsProxies.List(c.base.config.Project)

	resourceList, err := listCall.Do()
	if err != nil {
		log.Fatal(err)
	}

	for _, resource := range resourceList.Items {
		c.resourceMap.Store(resource.Name, nil)
	}

	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *ComputeTargetHTTPSProxies) Dependencies() []string {
	a := ComputeGlobalForwardingRules{}
	b := ComputeForwardingRules{}

	return []string{a.Name(), b.Name()}
}

// Remove -
func (c *ComputeTargetHTTPSProxies) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)

		// Parallel target https proxy deletion
		errs.Go(func() error {
			deleteCall := c.serviceClient.TargetHttpsProxies.Delete(c.base.config.Project, resourceID)

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

				operationCall := c.serviceClient.GlobalOperations.Get(c.base.config.Project, operation.Name)

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
