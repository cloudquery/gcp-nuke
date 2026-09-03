package gcp

import (
	"fmt"
	"log"
	"sync"

	"github.com/arehmandev/gcp-nuke/config"
	"github.com/arehmandev/gcp-nuke/helpers"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/syncmap"
	"google.golang.org/api/cloudkms/v1"
)

// KMSCryptoKeys - keys are not deletable in GCP, so this only removes the
// rotation schedule. Without that a rotating key mints a fresh billable version
// every rotation period and destroying versions never converges
type KMSCryptoKeys struct {
	serviceClient *cloudkms.Service
	resourceMap   syncmap.Map
	base          ResourceBase
}

func init() {
	kmsService, err := cloudkms.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}

	kmsResource := KMSCryptoKeys{
		serviceClient: kmsService,
	}

	register(&kmsResource)
}

// Name - Name of the resourceLister for KMSCryptoKeys
func (c *KMSCryptoKeys) Name() string {
	return "KMSCryptoKeys"
}

// ToSlice - Name of the resourceLister for KMSCryptoKeys
func (c *KMSCryptoKeys) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)
}

// Setup - populates the struct
func (c *KMSCryptoKeys) Setup(config config.Config) {
	c.base.config = config
}

// List - Returns a list of all KMSCryptoKeys still carrying a rotation schedule
func (c *KMSCryptoKeys) List(refreshCache bool) []string {
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

	for _, key := range keys {
		// once the schedule is gone the key drops out of this list
		if key.RotationPeriod == "" {
			continue
		}

		c.resourceMap.Store(key.Name, nil)
	}

	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *KMSCryptoKeys) Dependencies() []string {
	return []string{}
}

// Remove -
func (c *KMSCryptoKeys) Remove() error {
	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		resourceID := key.(string)

		// Parallel rotation schedule removal
		errs.Go(func() error {
			patchCall := c.serviceClient.Projects.Locations.KeyRings.CryptoKeys.Patch(
				resourceID,
				&cloudkms.CryptoKey{
					NullFields: []string{"RotationPeriod", "NextRotationTime"},
				},
			).UpdateMask("rotationPeriod,nextRotationTime")

			if _, err := patchCall.Do(); err != nil {
				return err
			}

			c.resourceMap.Delete(resourceID)

			log.Printf(
				"[Info] Rotation schedule removed %v [type: %v project: %v]",
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

// listKMSCryptoKeys - every crypto key in the project. Cloud KMS rejects the "-"
// location wildcard, and its locations are not the compute regions - global and
// us are KMS locations but not regions - so the locations have to be listed
func listKMSCryptoKeys(serviceClient *cloudkms.Service, config config.Config) ([]*cloudkms.CryptoKey, error) {
	locationsCall := serviceClient.Projects.Locations.List(fmt.Sprintf("projects/%v", config.Project))

	locations := []string{}

	err := locationsCall.Pages(config.Ctx, func(page *cloudkms.ListLocationsResponse) error {
		for _, location := range page.Locations {
			locations = append(locations, location.Name)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	var (
		mutex sync.Mutex
		keys  []*cloudkms.CryptoKey
	)

	errs, ctx := errgroup.WithContext(config.Ctx)

	for _, location := range locations {
		location := location

		errs.Go(func() error {
			keyRingsCall := serviceClient.Projects.Locations.KeyRings.List(location)

			return keyRingsCall.Pages(ctx, func(page *cloudkms.ListKeyRingsResponse) error {
				for _, keyRing := range page.KeyRings {
					cryptoKeysCall := serviceClient.Projects.Locations.KeyRings.CryptoKeys.List(keyRing.Name)

					err := cryptoKeysCall.Pages(ctx, func(page *cloudkms.ListCryptoKeysResponse) error {
						mutex.Lock()
						defer mutex.Unlock()

						keys = append(keys, page.CryptoKeys...)

						return nil
					})
					if err != nil {
						return err
					}
				}

				return nil
			})
		})
	}

	if err := errs.Wait(); err != nil {
		return nil, err
	}

	return keys, nil
}
