package packages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	packagedesc "platformserver/services/devops/descriptor"
)

const (
	MaxCatalogBytes            = 2 << 20
	SchemaPackageLibraryAdd    = "platform.package.library.add"
	SchemaPackageLibraryRemove = "platform.package.library.remove"
	OfficialLibraryID          = "official"
	OfficialLibraryTitle       = "Official library"
)

func OfficialLibraryURL() string {
	return (&url.URL{Scheme: "https", Host: "github.com", Path: "/xuciro-commits/platform-packages"}).String()
}

func OfficialLibrary() runtimeprotocol.PackageLibrary {
	raw := OfficialLibraryURL()
	resolved := ""
	if candidates, err := ResolveCatalogCandidates(raw); err == nil && len(candidates) > 0 {
		resolved = candidates[0]
	}
	return runtimeprotocol.PackageLibrary{ID: OfficialLibraryID, URL: raw, ResolvedURL: resolved, Title: OfficialLibraryTitle, Builtin: true}
}

func EffectiveLibraries(custom []runtimeprotocol.PackageLibrary) []runtimeprotocol.PackageLibrary {
	official := OfficialLibrary()
	officialKey := strings.TrimRight(official.URL, "/")
	out := make([]runtimeprotocol.PackageLibrary, 0, len(custom)+1)
	out = append(out, official)
	for _, lib := range custom {
		if strings.TrimRight(strings.TrimSpace(lib.URL), "/") == officialKey {
			continue
		}
		out = append(out, lib)
	}
	return out
}

var allowLoopbackPackages atomic.Bool

func SetAllowLoopback(allow bool) { allowLoopbackPackages.Store(allow) }

func AllowLoopback() bool {
	return allowLoopbackPackages.Load() || os.Getenv("PLATFORM_ALLOW_LOOPBACK_PACKAGES") == "1"
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ValidatePackageURL(raw string) (*url.URL, bool, error) {
	trimmed := strings.TrimSpace(raw)
	address, err := url.Parse(trimmed)
	if err != nil || address.Host == "" || address.User != nil || address.Fragment != "" {
		return nil, false, fmt.Errorf("Use an HTTPS address for a platform package, without embedded credentials")
	}
	switch strings.ToLower(address.Scheme) {
	case "https":
		return address, false, nil
	case "http":
		if AllowLoopback() && isLoopbackHost(address.Hostname()) {
			return address, true, nil
		}
	}
	return nil, false, fmt.Errorf("Use an HTTPS address for a platform package, without embedded credentials")
}

func LibraryID(rawURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(strings.TrimSpace(rawURL), "/")))
	return "lib-" + hex.EncodeToString(sum[:6])
}

func LibraryCacheKey(tenant, id string) string {
	return "packages/" + tenant + "/libraries/" + id + ".json"
}

type gitForge struct {
	host      string
	rawHost   string
	rawPrefix string
}

var catalogForges = []gitForge{
	{host: "github.com", rawHost: "raw.githubusercontent.com", rawPrefix: ""},
	{host: "gitee.com", rawHost: "gitee.com", rawPrefix: "raw/"},
}

func forgeRawURL(forge gitForge, owner, repo, branch, relPath string) string {
	u := url.URL{Scheme: "https", Host: forge.rawHost}
	u.Path = "/" + owner + "/" + repo + "/" + forge.rawPrefix + branch + "/" + strings.TrimLeft(relPath, "/")
	return u.String()
}

func ResolveCatalogCandidates(raw string) ([]string, error) {
	address, _, err := ValidatePackageURL(raw)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(address.Hostname())
	cleanPath := strings.Trim(address.Path, "/")
	segments := []string{}
	if cleanPath != "" {
		segments = strings.Split(cleanPath, "/")
	}
	for _, forge := range catalogForges {
		if host != forge.host || len(segments) < 2 {
			continue
		}
		owner, repo := segments[0], strings.TrimSuffix(segments[1], ".git")
		if len(segments) == 2 {
			return []string{
				forgeRawURL(forge, owner, repo, "main", "catalog.json"),
				forgeRawURL(forge, owner, repo, "master", "catalog.json"),
			}, nil
		}
		if segments[2] == "tree" && len(segments) >= 4 {
			branch := segments[3]
			sub := strings.Join(segments[4:], "/")
			rel := "catalog.json"
			if sub != "" {
				rel = sub + "/catalog.json"
			}
			return []string{forgeRawURL(forge, owner, repo, branch, rel)}, nil
		}
		if segments[2] == "blob" && len(segments) >= 5 {
			branch := segments[3]
			return []string{forgeRawURL(forge, owner, repo, branch, strings.Join(segments[4:], "/"))}, nil
		}
	}
	if strings.HasSuffix(strings.ToLower(address.Path), ".json") {
		return []string{address.String()}, nil
	}
	target := *address
	target.Path = strings.TrimRight(address.Path, "/") + "/catalog.json"
	return []string{target.String()}, nil
}

type catalogDocument struct {
	Version  int                                   `json:"version"`
	Title    string                                `json:"title,omitempty"`
	Packages []runtimeprotocol.PackageCatalogEntry `json:"packages"`
}

func normalizeSha256(raw string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return "", fmt.Errorf("A catalog entry must include a sha256 digest")
	}
	if !strings.HasPrefix(trimmed, "sha256:") {
		trimmed = "sha256:" + trimmed
	}
	if !platform.ValidDigest(trimmed) {
		return "", fmt.Errorf("Invalid sha256 digest in catalog entry")
	}
	return trimmed, nil
}

func DecodeCatalog(raw []byte, baseURL string) (string, []runtimeprotocol.PackageCatalogEntry, error) {
	if len(raw) == 0 || len(raw) > MaxCatalogBytes {
		return "", nil, fmt.Errorf("A library catalog must be non-empty and at most 2 MiB")
	}
	var doc catalogDocument
	if err := platform.DecodeJSON(raw, &doc, "library catalog"); err != nil {
		return "", nil, err
	}
	if doc.Version != 1 {
		return "", nil, fmt.Errorf("Unsupported library catalog version %d; expected 1", doc.Version)
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", nil, fmt.Errorf("Invalid catalog address")
	}
	seen := map[string]bool{}
	out := make([]runtimeprotocol.PackageCatalogEntry, 0, len(doc.Packages))
	for _, entry := range doc.Packages {
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Version = strings.TrimSpace(entry.Version)
		entry.Title = strings.TrimSpace(entry.Title)
		entry.Description = strings.TrimSpace(entry.Description)
		entry.URL = strings.TrimSpace(entry.URL)
		if entry.ID == "" || entry.Version == "" || entry.Title == "" || entry.URL == "" {
			return "", nil, fmt.Errorf("Each catalog entry requires id, version, title, url and sha256")
		}
		sum, err := normalizeSha256(entry.Sha256)
		if err != nil {
			return "", nil, err
		}
		entry.Sha256 = sum
		if entry.Listing != nil {
			if err := packagedesc.CheckListing(*entry.Listing); err != nil {
				return "", nil, fmt.Errorf("Catalog entry %s@%s: %s", entry.ID, entry.Version, err)
			}
		}
		if entry.Kind != "" && !slices.Contains(packagedesc.Kinds, entry.Kind) {
			return "", nil, fmt.Errorf("Catalog entry %s@%s: kind %q is not one of %s", entry.ID, entry.Version, entry.Kind, strings.Join(packagedesc.Kinds, ", "))
		}
		rel, err := url.Parse(entry.URL)
		if err != nil {
			return "", nil, fmt.Errorf("Catalog entry %s@%s has an invalid package address", entry.ID, entry.Version)
		}
		resolved := base.ResolveReference(rel).String()
		if _, _, err := ValidatePackageURL(resolved); err != nil {
			return "", nil, fmt.Errorf("Catalog entry %s@%s must use an HTTPS package address", entry.ID, entry.Version)
		}
		entry.URL = resolved
		key := entry.ID + "@" + entry.Version
		if seen[key] {
			return "", nil, fmt.Errorf("Duplicate package %s in library catalog", key)
		}
		seen[key] = true
		out = append(out, entry)
	}
	slices.SortFunc(out, func(a, b runtimeprotocol.PackageCatalogEntry) int {
		if a.ID != b.ID {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Version, b.Version)
	})
	return strings.TrimSpace(doc.Title), out, nil
}

type cachedLibrary struct {
	Title       string                                `json:"title,omitempty"`
	ResolvedURL string                                `json:"resolvedUrl,omitempty"`
	FetchedAt   time.Time                             `json:"fetchedAt"`
	Entries     []runtimeprotocol.PackageCatalogEntry `json:"entries"`
}

func loadCachedLibrary(ctx context.Context, tenant, id string, files platform.FileStore) (cachedLibrary, bool) {
	if files == nil {
		return cachedLibrary{}, false
	}
	rc, _, err := files.Get(ctx, LibraryCacheKey(tenant, id))
	if err != nil || rc == nil {
		return cachedLibrary{}, false
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, MaxCatalogBytes+1))
	if err != nil || len(raw) == 0 {
		return cachedLibrary{}, false
	}
	var cached cachedLibrary
	if json.Unmarshal(raw, &cached) != nil {
		return cachedLibrary{}, false
	}
	if cached.Entries == nil {
		cached.Entries = []runtimeprotocol.PackageCatalogEntry{}
	}
	return cached, true
}

func ReadLibraries(ctx context.Context, tenant string, libs []runtimeprotocol.PackageLibrary, files platform.FileStore) []runtimeprotocol.PackageLibraryView {
	out := make([]runtimeprotocol.PackageLibraryView, 0, len(libs))
	for _, lib := range libs {
		view := runtimeprotocol.PackageLibraryView{
			ID:          lib.ID,
			URL:         lib.URL,
			ResolvedURL: lib.ResolvedURL,
			Title:       lib.Title,
			Builtin:     lib.Builtin,
			Status:      "pending",
			Entries:     []runtimeprotocol.PackageCatalogEntry{},
		}
		if cached, ok := loadCachedLibrary(ctx, tenant, lib.ID, files); ok {
			view.Status = "ready"
			view.FetchedAt = cached.FetchedAt
			view.Entries = cached.Entries
			if cached.ResolvedURL != "" {
				view.ResolvedURL = cached.ResolvedURL
			}
			if view.Title == "" && cached.Title != "" {
				view.Title = cached.Title
			}
		}
		out = append(out, view)
	}
	return out
}

func FetchLibraryCatalog(ctx context.Context, tenant string, lib runtimeprotocol.PackageLibrary, send func(*http.Request, bool) (*http.Response, error), files platform.FileStore) runtimeprotocol.PackageLibraryView {
	candidates, err := ResolveCatalogCandidates(lib.URL)
	var fetchedTitle, resolvedURL string
	var entries []runtimeprotocol.PackageCatalogEntry
	if err == nil {
		var lastErr error
		for i, candidate := range candidates {
			address, allowPrivate, parseErr := ValidatePackageURL(candidate)
			if parseErr != nil {
				lastErr = parseErr
				continue
			}
			attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			resp, doErr := Download(attemptCtx, address, allowPrivate, send)
			if errors.Is(doErr, ErrUnreachable) {
				cancel()
				lastErr = fmt.Errorf("The library catalog could not be downloaded; check its address and try again")
				continue
			}
			if doErr != nil {
				cancel()
				lastErr = doErr
				break
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxCatalogBytes+1))
			resp.Body.Close()
			cancel()
			if resp.StatusCode == http.StatusNotFound && i+1 < len(candidates) {
				lastErr = fmt.Errorf("The library source answered HTTP 404")
				continue
			}
			if resp.StatusCode != http.StatusOK {
				lastErr = fmt.Errorf("The library source answered HTTP %d", resp.StatusCode)
				break
			}
			if readErr != nil {
				lastErr = fmt.Errorf("The library catalog could not be read")
				break
			}
			title, decoded, decErr := DecodeCatalog(body, address.String())
			if decErr != nil {
				lastErr = decErr
				break
			}
			fetchedTitle, resolvedURL, entries, lastErr = title, address.String(), decoded, nil
			break
		}
		err = lastErr
	}
	if err == nil {
		now := time.Now().UTC()
		cached := cachedLibrary{Title: fetchedTitle, ResolvedURL: resolvedURL, FetchedAt: now, Entries: entries}
		if raw, marshalErr := json.Marshal(cached); marshalErr == nil && files != nil {
			_ = files.Put(ctx, LibraryCacheKey(tenant, lib.ID), raw, "application/json")
		}
		title := lib.Title
		if title == "" {
			title = fetchedTitle
		}
		return runtimeprotocol.PackageLibraryView{
			ID:          lib.ID,
			URL:         lib.URL,
			ResolvedURL: resolvedURL,
			Title:       title,
			Builtin:     lib.Builtin,
			Status:      "ready",
			FetchedAt:   now,
			Entries:     entries,
		}
	}
	view := runtimeprotocol.PackageLibraryView{
		ID:          lib.ID,
		URL:         lib.URL,
		ResolvedURL: lib.ResolvedURL,
		Title:       lib.Title,
		Builtin:     lib.Builtin,
		Status:      "error",
		Error:       err.Error(),
		Entries:     []runtimeprotocol.PackageCatalogEntry{},
	}
	if cached, ok := loadCachedLibrary(ctx, tenant, lib.ID, files); ok {
		view.Status = "stale"
		view.FetchedAt = cached.FetchedAt
		view.Entries = cached.Entries
		if cached.ResolvedURL != "" {
			view.ResolvedURL = cached.ResolvedURL
		}
		if view.Title == "" && cached.Title != "" {
			view.Title = cached.Title
		}
	}
	return view
}

func RefreshLibraries(ctx context.Context, tenant string, libs []runtimeprotocol.PackageLibrary, targetID string, send func(*http.Request, bool) (*http.Response, error), files platform.FileStore) ([]runtimeprotocol.PackageLibraryView, error) {
	if targetID != "" && !slices.ContainsFunc(libs, func(lib runtimeprotocol.PackageLibrary) bool { return lib.ID == targetID }) {
		return nil, fmt.Errorf("The requested package library was not found")
	}
	out := make([]runtimeprotocol.PackageLibraryView, 0, len(libs))
	for _, lib := range libs {
		if targetID == "" || lib.ID == targetID {
			out = append(out, FetchLibraryCatalog(ctx, tenant, lib, send, files))
		} else {
			out = append(out, ReadLibraries(ctx, tenant, []runtimeprotocol.PackageLibrary{lib}, files)[0])
		}
	}
	return out, nil
}

func (d *Owner) PackageLibraries() []runtimeprotocol.PackageLibrary {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]runtimeprotocol.PackageLibrary, 0, len(d.packageLibraries))
	for _, lib := range d.packageLibraries {
		out = append(out, lib)
	}
	slices.SortFunc(out, func(a, b runtimeprotocol.PackageLibrary) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func (d *Owner) decidePackageLibrary(_ platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *platform.Error) {
	if s.GetSchema().GetName() == SchemaPackageLibraryRemove {
		var input struct{ ID string }
		_ = json.Unmarshal(s.GetPayload(), &input)
		id := strings.TrimSpace(input.ID)
		if id == "" {
			id = strings.TrimSpace(s.GetTarget().GetId())
		}
		if id == "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a package library to remove")
		}
		d.mu.Lock()
		_, found := d.packageLibraries[id]
		d.mu.Unlock()
		if !found {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The package library is unavailable")
		}
		return func(*pb.ChangeRecord) { delete(d.packageLibraries, id) }, nil
	}
	var input struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if json.Unmarshal(s.GetPayload(), &input) != nil || strings.TrimSpace(input.URL) == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Enter a package library address")
	}
	candidates, err := ResolveCatalogCandidates(input.URL)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{reason}", err.Error())
	}
	cleanURL := strings.TrimSpace(input.URL)
	id := LibraryID(cleanURL)
	resolved := ""
	if len(candidates) > 0 {
		resolved = candidates[0]
	}
	title := strings.TrimSpace(input.Title)
	return func(r *pb.ChangeRecord) {
		if d.packageLibraries == nil {
			d.packageLibraries = map[string]runtimeprotocol.PackageLibrary{}
		}
		added := time.Now().UTC()
		if r != nil && r.GetRecordedTime() != nil {
			added = r.GetRecordedTime().AsTime()
		}
		d.packageLibraries[id] = runtimeprotocol.PackageLibrary{
			ID:          id,
			URL:         cleanURL,
			ResolvedURL: resolved,
			Title:       title,
			AddedAt:     added,
		}
	}, nil
}
