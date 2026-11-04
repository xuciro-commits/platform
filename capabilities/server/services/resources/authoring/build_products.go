package build

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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

func (p Product) Check() error {
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
	return (platform.PackageDescriptor{ID: p.Name, Version: "0.0.0", Namespace: namespace, Requires: p.Requires}).Validate()
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

func (b *Build) productEntity() platform.Entity {
	return platform.Entity{
		Type: ProductType, Title: "Product", Plural: "Products", Model: Product{}, Display: "title",
		Description: "A product chooses the project resources that DevOps packages into immutable versions.",
		Scope:       platform.Scope{Default: platform.ScopeTenant, Levels: map[string]string{Builder: platform.ScopeTenant, Publisher: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Publisher}, Capability: "releases"},
		Validate: func(c platform.Caller, value any) *kernel.Error {
			product, ok := value.(*Product)
			if !ok {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The product definition is unreadable")
			}
			if product.Namespace == "" {
				product.Namespace = product.Name
			}
			if err := product.Check(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{reason}", err.Error())
			}
			if prior, exists := platform.Get[Product](c, product.ID); exists && prior.Project != product.Project {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A product stays in the project that owns its contents")
			}
			project, exists := platform.Get[Application](c, product.Project)
			if !exists || project.Archived || !canEditProject(c, product.Project) {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Choose a project you can edit")
			}
			for _, root := range product.Roots {
				resource, exists := platform.Get[Resource](c, platform.ResourceID(c.Tenant, root))
				if !exists || resource.Archived || resource.Project != product.Project {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Product contents must be resources in its project")
				}
			}
			return nil
		},
	}
}

func (b *Build) productVersionEntity() platform.Entity {
	return platform.Entity{
		Type: ProductVersionType, Title: "Product version", Plural: "Product versions", Model: ProductVersion{}, Display: "version",
		Description: "A frozen product version references one saved release candidate and keeps the exact package contents.",
		Scope:       platform.Scope{Default: platform.ScopeTenant, Levels: map[string]string{Builder: platform.ScopeTenant, Publisher: platform.ScopeTenant}},
	}
}

func productVersionActions() []platform.Action {
	return []platform.Action{{
		Schema: SchemaProductVersion, Target: ProductVersionType, Capability: "releases",
		Title: "Publish product version", Description: "Freeze one saved release candidate as an immutable semantic product version.",
		Roles: []string{Builder, Publisher},
		Payload: []platform.Field{
			{Name: "product", Type: "string", Required: true, Description: "The product record ID"},
			{Name: "version", Type: "string", Required: true, Description: "A semantic version such as 1.2.0"},
			{Name: "candidate", Type: "string", Required: true, Description: "The saved release candidate ID"},
		},
	}}
}

func (b *Build) submitProductVersion(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if !c.Replaying && !c.Holds(ID, Builder) && !c.Holds(ID, Publisher) && !c.Holds("platform", "admin") {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Publishing a product version requires a builder or publisher role")
	}
	var input struct {
		Product   string `json:"product"`
		Version   string `json:"version"`
		Candidate string `json:"candidate"`
	}
	if json.Unmarshal(s.GetPayload(), &input) != nil || input.Product == "" || input.Version == "" || input.Candidate == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a product, semantic version and saved release candidate")
	}
	if s.GetTarget().GetId() != input.Product+"@"+input.Version {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A product version ID combines its product and version")
	}
	product, exists := platform.Get[Product](c, input.Product)
	if !exists || product.Archived {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The product is unavailable")
	}
	if product.Record.ID != product.Name {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The product record identity differs from its API name")
	}
	if !canEditProject(c, product.Project) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Publishing a product version requires editing its project")
	}
	if _, exists := platform.Get[ProductVersion](c, s.GetTarget().GetId()); exists {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A product version cannot be replaced; publish another version")
	}
	frozen, err := b.host.ProductVersion(platform.ProductVersionRequest{
		ID: product.Name, Version: input.Version, Title: product.Title, Namespace: product.Namespace,
		Project: product.Project, Roots: slices.Clone(product.Roots), Requires: slices.Clone(product.Requires),
		Candidate: input.Candidate, At: now.UTC(),
	})
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The product version cannot be frozen: {reason}", err.Error())
	}
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return func(r *pb.ChangeRecord) {
			if _, exists := platform.Get[ProductVersion](c, s.GetTarget().GetId()); exists {
				c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A product version cannot be replaced"))
				return
			}
			c.Put(r, ProductVersion{
				Record:          platform.Record{ID: s.GetTarget().GetId()},
				Product:         product.Name,
				Version:         frozen.Version,
				Candidate:       frozen.Candidate,
				CandidateDigest: frozen.CandidateDigest,
				ContentDigest:   frozen.ContentDigest,
				Descriptor:      frozen.Descriptor,
			})
		}, nil
	})
}
