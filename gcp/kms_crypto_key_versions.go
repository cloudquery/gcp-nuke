package gcp

import (
	"log"
	"sync"

	"github.com/arehmandev/gcp-nuke/config"
	"github.com/arehmandev/gcp-nuke/helpers"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/syncmap"
	"google.golang.org/api/cloudkms/v1"
)

// KMSCryptoKeyVersions - key versions are the billed unit, and the only part of
// Cloud KMS that can be removed at all. Keyrings and keys stay in the project
// permanently, so this stops the charge rather than tidying the estate
type KMSCryptoKeyVersions struct {
	serviceClient *cloudkms.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	kmsService, err := cloudkms.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	kmsResource := KMSCryptoKeyVersions{
		serviceClient: kmsService,
	}

	register(&kmsResource)
}

// Name - Name of the resourceLister for KMSCryptoKeyVersions
func (c *KMSCryptoKeyVersions) Name() string {
	return "KMSCryptoKeyVersions"
}

// ToSlice - Name of the resourceLister for KMSCryptoKeyVersions
func (c *KMSCryptoKeyVersions) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *KMSCryptoKeyVersions) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all destroyable KMSCryptoKeyVersions
func (c *KMSCryptoKeyVersions) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	keys, err := listKMSCryptoKeys(c.serviceClient, c.base.config)
	if err != nil {
		if isAPIUnavailable(err) {
			log.Printf("[Info] Skipping %v - API unavailable on project %v", c.Name(), c.base.config.Project)
			return c.ToSlice()
		}

		log.Fatal(err)
	}

	errs, ctx := errgroup.WithContext(c.base.config.Ctx)

	for _, key := range keys {
		key := key

		errs.Go(func() error {
			listCall := c.serviceClient.Projects.Locations.KeyRings.CryptoKeys.CryptoKeyVersions.List(key.Name)

			return listCall.Pages(ctx, func(page *cloudkms.ListCryptoKeyVersionsResponse) error {
				for _, version := range page.CryptoKeyVersions {
					// a scheduled or completed destroy would keep this list non
					// empty for the whole 24 hour window and beyond
					if version.State != "ENABLED" && version.State != "DISABLED" {
						continue
					}

					c.resourceMap.Store(version.Name, nil)
				}

				return nil
			})
		})
	}

	if err := errs.Wait(); err != nil {
		log.Fatal(err)
	}

	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *KMSCryptoKeyVersions) Dependencies() []string {
	a := KMSCryptoKeys{}

	return []string{a.Name()}
}

// Remove -
func (c *KMSCryptoKeyVersions) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)

		// Parallel key version destruction
		errs.Go(func() error {
			destroyCall := c.serviceClient.Projects.Locations.KeyRings.CryptoKeys.CryptoKeyVersions.Destroy(
				resourceID,
				&cloudkms.DestroyCryptoKeyVersionRequest{},
			)

			// destruction is scheduled rather than immediate, so there is no
			// operation to poll - the version moves to DESTROY_SCHEDULED and
			// stays recoverable with versions restore until the window elapses
			if _, err := destroyCall.Do(); err != nil {
				return err
			}

			c.resourceMap.Delete(resourceID)

			log.Printf(
				"[Info] Resource scheduled for destruction %v [type: %v project: %v]",
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
