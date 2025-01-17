package catalogmetadata

import (
	"encoding/json"
	"fmt"
	"sync"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
)

const (
	MediaTypePlain          = "plain+v0"
	MediaTypeRegistry       = "registry+v1"
	PropertyBundleMediaType = "olm.bundle.mediatype"
)

type Schemas interface {
	Package | Bundle | Channel
}

type Package struct {
	declcfg.Package
}

type Channel struct {
	declcfg.Channel
}

type PackageRequired struct {
	property.PackageRequired
	SemverRange bsemver.Range `json:"-"`
}

type Bundle struct {
	declcfg.Bundle
	CatalogName string
	InChannels  []*Channel

	mu sync.RWMutex
	// these properties are lazy loaded as they are requested
	propertiesMap    map[string][]*property.Property
	bundlePackage    *property.Package
	semVersion       *bsemver.Version
	requiredPackages []PackageRequired
	mediaType        *string
}

func (b *Bundle) Version() (*bsemver.Version, error) {
	if err := b.loadPackage(); err != nil {
		return nil, err
	}
	return b.semVersion, nil
}

func (b *Bundle) RequiredPackages() ([]PackageRequired, error) {
	if err := b.loadRequiredPackages(); err != nil {
		return nil, err
	}
	return b.requiredPackages, nil
}

func (b *Bundle) MediaType() (string, error) {
	if err := b.loadMediaType(); err != nil {
		return "", err
	}

	return *b.mediaType, nil
}

func (b *Bundle) loadPackage() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bundlePackage == nil {
		bundlePackage, err := loadOneFromProps[property.Package](b, property.TypePackage, true)
		if err != nil {
			return err
		}
		b.bundlePackage = &bundlePackage
	}
	if b.semVersion == nil {
		semVer, err := bsemver.Parse(b.bundlePackage.Version)
		if err != nil {
			return fmt.Errorf("could not parse semver %q for bundle '%s': %s", b.bundlePackage.Version, b.Name, err)
		}
		b.semVersion = &semVer
	}
	return nil
}

func (b *Bundle) loadRequiredPackages() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requiredPackages == nil {
		requiredPackages, err := loadFromProps[PackageRequired](b, property.TypePackageRequired, false)
		if err != nil {
			return fmt.Errorf("error determining bundle required packages for bundle %q: %s", b.Name, err)
		}
		for i := range requiredPackages {
			semverRange, err := bsemver.ParseRange(requiredPackages[i].VersionRange)
			if err != nil {
				return fmt.Errorf(
					"error parsing bundle required package semver range for bundle %q (required package %q): %s",
					b.Name,
					requiredPackages[i].PackageName,
					err,
				)
			}
			requiredPackages[i].SemverRange = semverRange
		}
		b.requiredPackages = requiredPackages
	}
	return nil
}

func (b *Bundle) loadMediaType() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mediaType == nil {
		mediaType, err := loadOneFromProps[string](b, PropertyBundleMediaType, false)
		if err != nil {
			return fmt.Errorf("error determining bundle mediatype for bundle %q: %s", b.Name, err)
		}
		b.mediaType = &mediaType
	}
	return nil
}

func (b *Bundle) propertiesByType(propType string) []*property.Property {
	if b.propertiesMap == nil {
		b.propertiesMap = make(map[string][]*property.Property)
		for i := range b.Properties {
			prop := b.Properties[i]
			b.propertiesMap[prop.Type] = append(b.propertiesMap[prop.Type], &prop)
		}
	}

	return b.propertiesMap[propType]
}

func loadOneFromProps[T any](bundle *Bundle, propType string, required bool) (T, error) {
	r, err := loadFromProps[T](bundle, propType, required)
	if err != nil {
		return *new(T), err
	}
	if len(r) > 1 {
		return *new(T), fmt.Errorf("expected 1 instance of property with type %q, got %d", propType, len(r))
	}
	if !required && len(r) == 0 {
		return *new(T), nil
	}

	return r[0], nil
}

func loadFromProps[T any](bundle *Bundle, propType string, required bool) ([]T, error) {
	props := bundle.propertiesByType(propType)
	if len(props) != 0 {
		result := []T{}
		for i := range props {
			parsedProp := *new(T)
			if err := json.Unmarshal(props[i].Value, &parsedProp); err != nil {
				return nil, fmt.Errorf("property %q with value %q could not be parsed: %s", propType, props[i].Value, err)
			}
			result = append(result, parsedProp)
		}
		return result, nil
	} else if required {
		return nil, fmt.Errorf("bundle property with type %q not found", propType)
	}

	return nil, nil
}
