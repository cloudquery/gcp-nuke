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

// PrivateCACertificateAuthorities -
type PrivateCACertificateAuthorities struct {
	serviceClient *privateca.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	privateCAService, err := privateca.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	privateCAResource := PrivateCACertificateAuthorities{
		serviceClient: privateCAService,
	}

	register(&privateCAResource)
}

// Name - Name of the resourceLister for PrivateCACertificateAuthorities
func (c *PrivateCACertificateAuthorities) Name() string {
	return "PrivateCACertificateAuthorities"
}

// ToSlice - Name of the resourceLister for PrivateCACertificateAuthorities
func (c *PrivateCACertificateAuthorities) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *PrivateCACertificateAuthorities) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all PrivateCACertificateAuthorities
func (c *PrivateCACertificateAuthorities) List(refreshCache bool) []string {
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
		listCall := c.serviceClient.Projects.Locations.CaPools.CertificateAuthorities.List(pool)

		err := listCall.Pages(c.base.config.Ctx, func(page *privateca.ListCertificateAuthoritiesResponse) error {
			for _, ca := range page.CertificateAuthorities {
				// a deleted authority lingers for its grace period, and would
				// keep this list non empty for the whole 30 days
				if ca.State == "DELETED" {
					continue
				}

				c.resourceMap.Store(ca.Name, ca.State)
			}

			return nil
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *PrivateCACertificateAuthorities) Dependencies() []string {
	return []string{}
}

// Remove -
func (c *PrivateCACertificateAuthorities) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)
		state := value.(string)

		// Parallel certificate authority deletion
		errs.Go(func() error {
			// disabling is what stops the per-authority devops tier charge, and
			// it takes effect immediately rather than after the grace period
			if state != "DISABLED" {
				disableCall := c.serviceClient.Projects.Locations.CaPools.CertificateAuthorities.Disable(
					resourceID,
					&privateca.DisableCertificateAuthorityRequest{IgnoreDependentResources: true},
				)

				disableOperation, err := disableCall.Do()
				if err != nil {
					return err
				}

				if err := c.waitForOperation(disableOperation, resourceID); err != nil {
					return err
				}
			}

			// the 30 day grace period is deliberately kept, so a wrongly swept
			// authority can still be recovered with roots undelete. List drops
			// authorities in the DELETED state so the wait loop still finishes
			deleteCall := c.serviceClient.Projects.Locations.CaPools.CertificateAuthorities.Delete(resourceID).
				IgnoreActiveCertificates(true).
				IgnoreDependentResources(true)

			operation, err := deleteCall.Do()
			if err != nil {
				return err
			}

			if err := c.waitForOperation(operation, resourceID); err != nil {
				return err
			}

			c.resourceMap.Delete(resourceID)

			log.Printf(
				"[Info] Resource deleted %v [type: %v project: %v]",
				resourceID,
				c.Name(),
				c.base.config.Project,
			)

			return nil
		})

		return true
	})

	// Wait for all deletions to complete, and return the first non nil error
	err := errs.Wait()
	return err
}

func (c *PrivateCACertificateAuthorities) waitForOperation(operation *privateca.Operation, resourceID string) error {
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

	return nil
}
