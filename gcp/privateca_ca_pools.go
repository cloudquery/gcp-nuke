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
	"google.golang.org/api/privateca/v1"
)

// PrivateCACAPools -
type PrivateCACAPools struct {
	serviceClient *privateca.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	privateCAService, err := privateca.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	privateCAResource := PrivateCACAPools{
		serviceClient: privateCAService,
	}

	register(&privateCAResource)
}

// Name - Name of the resourceLister for PrivateCACAPools
func (c *PrivateCACAPools) Name() string {
	return "PrivateCACAPools"
}

// ToSlice - Name of the resourceLister for PrivateCACAPools
func (c *PrivateCACAPools) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *PrivateCACAPools) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all PrivateCACAPools
func (c *PrivateCACAPools) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	pools, err := listCAPools(c.serviceClient, c.base.config)
	if err != nil {
		if isAPIUnavailable(err) {
			log.Printf("[Info] Skipping %v - API unavailable on project %v", c.Name(), c.base.config.Project)
			return c.ToSlice()
		}

		log.Fatal(err)
	}

	for _, pool := range pools {
		// deleting an authority keeps it recoverable for its grace period, and
		// GCP refuses to delete a pool that still holds one. Listing the pool
		// would fail the sweep every run until the window elapses
		recovering, err := poolHoldsRecoveringCA(c.serviceClient, c.base.config, pool)
		if err != nil {
			log.Fatal(err)
		}

		if recovering {
			log.Printf("[Info] Skipping %v - holds an authority still in its recovery period", pool)
			continue
		}

		c.resourceMap.Store(pool, nil)
	}

	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *PrivateCACAPools) Dependencies() []string {
	a := PrivateCACertificateAuthorities{}

	return []string{a.Name()}
}

// Remove -
func (c *PrivateCACAPools) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)

		// Parallel ca pool deletion
		errs.Go(func() error {
			deleteCall := c.serviceClient.Projects.Locations.CaPools.Delete(resourceID).
				IgnoreDependentResources(true)

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

				if err := privateCAOperationError(checkOpp); err != nil {
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

// listCAPools - resource names of every ca pool in the project, across locations
func listCAPools(serviceClient *privateca.Service, config config.Config) ([]string, error) {
	parent := fmt.Sprintf("projects/%v/locations/-", config.Project)

	pools := []string{}

	listCall := serviceClient.Projects.Locations.CaPools.List(parent)

	err := listCall.Pages(config.Ctx, func(page *privateca.ListCaPoolsResponse) error {
		for _, pool := range page.CaPools {
			pools = append(pools, pool.Name)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return pools, nil
}

// poolHoldsRecoveringCA - whether a pool still holds a deleted authority inside
// its recovery period, which blocks deletion of the pool itself
func poolHoldsRecoveringCA(serviceClient *privateca.Service, config config.Config, pool string) (bool, error) {
	listCall := serviceClient.Projects.Locations.CaPools.CertificateAuthorities.List(pool)

	recovering := false

	err := listCall.Pages(config.Ctx, func(page *privateca.ListCertificateAuthoritiesResponse) error {
		for _, ca := range page.CertificateAuthorities {
			if ca.State == "DELETED" {
				recovering = true
			}
		}

		return nil
	})
	if err != nil {
		return false, err
	}

	return recovering, nil
}
