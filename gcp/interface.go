package gcp

import (
	"context"
	"log"
	"strings"

	"github.com/arehmandev/gcp-nuke/config"
	"google.golang.org/api/compute/v1"
)

// ResourceBase -
type ResourceBase struct {
	config config.Config
}

// DefaultResourceProperties -
type DefaultResourceProperties struct {
	zone   string
	region string
}

// Resource -
type Resource interface {
	Name() string
	ToSlice() []string
	Setup(config config.Config)
	List(useCache bool) []string
	Dependencies() []string
	Remove() error
}

// Ctx = context
var (
	Ctx         = context.Background()
	resourceMap = make(map[string]Resource)
)

func register(resource Resource) {
	_, exists := resourceMap[resource.Name()]
	if exists {
		log.Fatalf("a resource with the name %s already exists", resource.Name())
	}

	resourceMap[resource.Name()] = resource
}

// GetResourceMap -
func GetResourceMap(config config.Config) map[string]Resource {
	for _, resource := range resourceMap {
		resource.Setup(config)
	}

	return resourceMap
}

// GetZones -
func GetZones(ctx context.Context, project string) []string {
	log.Println("[Info] Retrieving zones for project:", project)

	serviceClient, err := compute.NewService(ctx)
	if err != nil {
		log.Fatal(err)
	}

	zoneListCall := serviceClient.Zones.List(project)

	zoneList, err := zoneListCall.Do()
	if err != nil {
		log.Fatal(err)
	}

	zoneStringSlice := []string{}

	for _, zone := range zoneList.Items {
		zoneNameSplit := strings.Split(zone.Name, "/")
		zoneStringSlice = append(zoneStringSlice, zoneNameSplit[len(zoneNameSplit)-1])
	}

	return zoneStringSlice
}

// GetRegions -
func GetRegions(ctx context.Context, project string) []string {
	log.Println("[Info] Retrieving regions for project:", project)

	serviceClient, err := compute.NewService(ctx)
	if err != nil {
		log.Fatal(err)
	}

	regionListCall := serviceClient.Regions.List(project)

	regionList, err := regionListCall.Do()
	if err != nil {
		log.Fatal(err)
	}

	regionStringSlice := []string{}

	for _, region := range regionList.Items {
		regionNameSplit := strings.Split(region.Name, "/")
		regionStringSlice = append(regionStringSlice, regionNameSplit[len(regionNameSplit)-1])
	}

	return regionStringSlice
}

func extractGKESelfLink(input string) string {
	var (
		selfLinkSlice []string
		startAppend   bool
	)

	for _, word := range strings.Split(input, "/") {
		if word == "projects" {
			startAppend = true
		}

		if word == "zones" {
			word = "locations"
		}

		if startAppend {
			selfLinkSlice = append(selfLinkSlice, word)
		}
	}

	return strings.Join(selfLinkSlice, "/")
}
