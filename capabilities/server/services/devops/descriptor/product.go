package descriptor

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"platformserver/platform"
)

const ProductType = "build.product"
const ProductVersionType = "build.productversion"
const SchemaProductVersion = ProductVersionType + ".create"

var productName = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)

type Product struct {
	platform.Record
	Name        string              `json:"name" field:"required,search" title:"Product name"`
	Title       string              `json:"title" field:"required,search"`
	Description string              `json:"description,omitempty"`
	Project     string              `json:"project" field:"required"`
	Namespace   string              `json:"namespace,omitempty"`
	Roots       []platform.AssetRef `json:"roots" field:"required" type:"json"`
	Requires    []string            `json:"requires,omitempty" type:"json"`
}

func (p Product) Check(withDependencies func(platform.ReleaseAsset) (platform.ReleaseAsset, error)) error {
	if !productName.MatchString(p.Name) || strings.TrimSpace(p.Title) == "" || len(p.Title) > 256 || len(p.Description) > 4096 {
		return fmt.Errorf("a product needs a lower-case name and a title")
	}
	if len(p.Roots) == 0 || len(p.Roots) > 1000 || len(p.Requires) > 64 {
		return fmt.Errorf("a product needs one to 1000 contents and at most 64 dependency ranges")
	}
	seen := map[platform.AssetRef]bool{}
	for _, root := range p.Roots {
		if root.Check() != nil || root.Kind == platform.AssetProduct || seen[root] {
			return fmt.Errorf("product contents need unique releaseable resources")
		}
		seen[root] = true
	}
	namespace := p.Namespace
	if namespace == "" {
		namespace = p.Name
	}
	return Validate(platform.PackageDescriptor{ID: p.Name, Version: "0.0.0", Namespace: namespace, Requires: p.Requires}, withDependencies)
}

type ProductVersion struct {
	platform.Record
	Product         string                     `json:"product" field:"readonly,search"`
	Version         string                     `json:"version" field:"readonly,search"`
	Candidate       string                     `json:"candidate" field:"readonly"`
	CandidateDigest string                     `json:"candidateDigest" field:"readonly"`
	ContentDigest   string                     `json:"contentDigest" field:"readonly"`
	Descriptor      platform.PackageDescriptor `json:"descriptor" field:"readonly" type:"json"`
}

func ProductVersionEntity() platform.Entity {
	return platform.Entity{
		Type: ProductVersionType, Title: "Product version", Plural: "Product versions", Model: ProductVersion{}, Display: "version",
		Description: "A frozen product version references one saved release candidate and keeps the exact package contents.",
		Scope:       platform.Scope{Default: platform.ScopeTenant, Levels: map[string]string{"builder": platform.ScopeTenant, "publisher": platform.ScopeTenant}},
	}
}

func ProductVersionActions() []platform.Action {
	return []platform.Action{{
		Schema: SchemaProductVersion, Target: ProductVersionType, Capability: "releases",
		Title: "Publish product version", Description: "Freeze one saved release candidate as an immutable semantic product version.",
		Roles: []string{"builder", "publisher"},
		Payload: []platform.Field{
			{Name: "product", Type: "string", Required: true, Description: "The product record ID"},
			{Name: "version", Type: "string", Required: true, Description: "A semantic version such as 1.2.0"},
			{Name: "candidate", Type: "string", Required: true, Description: "The saved release candidate ID"},
		},
	}}
}

func ProductVersionViews(rows []ProductVersion) []platform.ProductVersionView {
	out := []platform.ProductVersionView{}
	for _, version := range rows {
		out = append(out, platform.ProductVersionView{Product: version.Product, Version: version.Version, Candidate: version.Candidate, Descriptor: version.Descriptor})
	}
	slices.SortFunc(out, func(a, b platform.ProductVersionView) int {
		if a.Product != b.Product {
			return strings.Compare(a.Product, b.Product)
		}
		return ComparePackageVersions(a.Version, b.Version)
	})
	return out
}
