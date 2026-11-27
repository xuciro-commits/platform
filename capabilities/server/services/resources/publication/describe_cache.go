package publication

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"slices"
	"sync"

	"platformserver/platform"
)

const describeCacheLimit = 8192

type describeKey [sha256.Size]byte

type describeAnswer struct {
	dependencies platform.Dependencies
	err          error
}

type DescribeCache struct {
	mu      sync.Mutex
	answers map[describeKey]describeAnswer
}

func NewDescribeCache() *DescribeCache {
	return &DescribeCache{answers: map[describeKey]describeAnswer{}}
}

func describeKeyOf(asset platform.ReleaseAsset, historical bool) describeKey {
	h := sha256.New()
	field := func(b []byte) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		h.Write(n[:])
		h.Write(b)
	}
	field([]byte(asset.Ref.App))
	field([]byte(asset.Ref.Kind))
	field([]byte(asset.Ref.Name))
	field([]byte(asset.SourceVersion))
	requires, _ := json.Marshal(asset.Requires)
	field(requires)
	field(asset.Body)
	var flags [9]byte
	if historical {
		flags[0] = 1
	}
	binary.BigEndian.PutUint64(flags[1:], platform.HostedGeneration())
	field(flags[:])
	var key describeKey
	h.Sum(key[:0])
	return key
}

func cloneDependencies(d platform.Dependencies) platform.Dependencies {
	d.Requires = slices.Clone(d.Requires)
	d.Provides = slices.Clone(d.Provides)
	for i := range d.Provides {
		d.Provides[i].Roles = slices.Clone(d.Provides[i].Roles)
		d.Provides[i].Parameters = slices.Clone(d.Provides[i].Parameters)
	}
	return d
}

func (c *DescribeCache) describe(asset platform.ReleaseAsset, historical bool, fresh func() (platform.Dependencies, error)) (platform.Dependencies, error) {
	key := describeKeyOf(asset, historical)
	c.mu.Lock()
	answer, hit := c.answers[key]
	c.mu.Unlock()
	if hit {
		return cloneDependencies(answer.dependencies), answer.err
	}
	dependencies, err := fresh()
	c.mu.Lock()
	if len(c.answers) >= describeCacheLimit {
		clear(c.answers)
	}
	c.answers[key] = describeAnswer{dependencies: cloneDependencies(dependencies), err: err}
	c.mu.Unlock()
	return dependencies, err
}

func (resolver Resolver) describeAsset(asset platform.ReleaseAsset, historical bool) (platform.Dependencies, error) {
	if resolver.Describes == nil {
		return resolver.describeAssetFresh(asset, historical)
	}
	return resolver.Describes.describe(asset, historical, func() (platform.Dependencies, error) {
		return resolver.describeAssetFresh(asset, historical)
	})
}
