package packages

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type cachedDirIndex struct {
	stamp string
	index *protocol.PackageIndex
}

var dirIndexCache struct {
	mu      sync.Mutex
	entries map[string]cachedDirIndex
}

func directoryStamp(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		parts = append(parts, entry.Name()+"|"+strconv.FormatInt(info.Size(), 10)+"|"+strconv.FormatInt(info.ModTime().UnixNano(), 10))
	}
	slices.Sort(parts)
	return strings.Join(parts, "\n"), nil
}

func loadCachedPackageIndex(resolver publication.Resolver, dir string) (*protocol.PackageIndex, string, error) {
	stamp, err := directoryStamp(dir)
	if err != nil {
		return nil, "", fmt.Errorf("package index %s: %w", dir, err)
	}
	dirIndexCache.mu.Lock()
	if cached, ok := dirIndexCache.entries[dir]; ok && cached.stamp == stamp {
		dirIndexCache.mu.Unlock()
		return cached.index, stamp, nil
	}
	dirIndexCache.mu.Unlock()

	loaded, err := LoadPackageIndex(resolver, dir)
	if err != nil {
		return nil, stamp, err
	}
	dirIndexCache.mu.Lock()
	if dirIndexCache.entries == nil {
		dirIndexCache.entries = map[string]cachedDirIndex{}
	}
	dirIndexCache.entries[dir] = cachedDirIndex{stamp: stamp, index: loaded}
	dirIndexCache.mu.Unlock()
	return loaded, stamp, nil
}

func (d *Owner) viewsFingerprint(dirStamp string, bound *protocol.PackageIndex) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write(bound.Directory)
	write(dirStamp)
	if bound.Directory == "" {
		for _, p := range bound.Packages {
			write(p.ID + "@" + p.Version + "@" + p.ArtifactDigest)
		}
	}
	d.mu.Lock()
	pkgIDs := make([]string, 0, len(d.packages))
	for id := range d.packages {
		pkgIDs = append(pkgIDs, id)
	}
	slices.Sort(pkgIDs)
	for _, id := range pkgIDs {
		p := d.packages[id]
		if p == nil {
			continue
		}
		write(fmt.Sprintf("pkg:%s@%s:%s:%s:%s:%t", p.ID, p.Version, p.State, p.Artifact, p.Space, p.DemoCleared))
		for _, r := range p.Retained {
			write("ret:" + r.Version + ":" + r.Digest)
		}
	}
	digests := make([]string, 0, len(d.packageSources))
	for digest := range d.packageSources {
		digests = append(digests, digest)
	}
	slices.Sort(digests)
	for _, digest := range digests {
		s := d.packageSources[digest]
		write("src:" + digest + ":" + s.ID + "@" + s.Version + ":" + s.Origin)
	}
	d.mu.Unlock()

	if d.host != nil {
		products := d.host.ProductVersions()
		slices.SortFunc(products, func(a, b platform.ProductVersionView) int {
			if a.Product != b.Product {
				return strings.Compare(a.Product, b.Product)
			}
			return strings.Compare(a.Version, b.Version)
		})
		for _, v := range products {
			write("prod:" + v.Product + "@" + v.Version + ":" + v.Candidate)
		}
		for _, app := range d.host.Apps() {
			m := app.Manifest()
			write(fmt.Sprintf("app:%s:%s:%d:%d", m.ID, m.Version, len(m.Entities), len(m.Pages)))
		}
		for _, mod := range d.host.BaseModules() {
			write(fmt.Sprintf("mod:%s:%s:%d", mod.ID, mod.Version, len(mod.Provides)))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
