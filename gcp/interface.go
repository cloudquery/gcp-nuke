package gcp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/arehmandev/gcp-nuke/config"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/networkmanagement/v1"
	"google.golang.org/api/networkservices/v1"
	"google.golang.org/api/privateca/v1"
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

// isAPIUnavailable - whether the error means the service is not usable on this
// project at all, rather than a listing failure the caller should know about
func isAPIUnavailable(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		if apiErr.Code == http.StatusForbidden || apiErr.Code == http.StatusNotFound {
			return true
		}
	}

	return strings.Contains(err.Error(), "has not been used in project") ||
		strings.Contains(err.Error(), "SERVICE_DISABLED")
}

// computeOperationError - a compute operation reports DONE whether it succeeded
// or failed, so the error has to be read rather than inferred from the status
func computeOperationError(operation *compute.Operation) error {
	if operation == nil || operation.Error == nil || len(operation.Error.Errors) == 0 {
		return nil
	}

	first := operation.Error.Errors[0]

	return fmt.Errorf("%v: %v", first.Code, first.Message)
}

// privateCAOperationError - as computeOperationError, for private ca
func privateCAOperationError(operation *privateca.Operation) error {
	if operation == nil || operation.Error == nil {
		return nil
	}

	return fmt.Errorf("%v: %v", operation.Error.Code, operation.Error.Message)
}

// networkServicesOperationError - as computeOperationError, for network services
func networkServicesOperationError(operation *networkservices.Operation) error {
	if operation == nil || operation.Error == nil {
		return nil
	}

	return fmt.Errorf("%v: %v", operation.Error.Code, operation.Error.Message)
}

// networkManagementOperationError - as computeOperationError, for network management
func networkManagementOperationError(operation *networkmanagement.Operation) error {
	if operation == nil || operation.Error == nil {
		return nil
	}

	return fmt.Errorf("%v: %v", operation.Error.Code, operation.Error.Message)
}

// containerOperationError - as computeOperationError, for kubernetes engine
func containerOperationError(operation *container.Operation) error {
	if operation == nil {
		return nil
	}

	if operation.Error != nil {
		return fmt.Errorf("%v: %v", operation.Error.Code, operation.Error.Message)
	}

	if operation.StatusMessage != "" {
		return fmt.Errorf("%v", operation.StatusMessage)
	}

	return nil
}
