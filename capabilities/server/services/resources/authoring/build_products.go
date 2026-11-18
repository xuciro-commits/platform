package authoring

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	packagedesc "platformserver/services/devops/descriptor"
)

type Product = packagedesc.Product
type ProductVersion = packagedesc.ProductVersion

const (
	ProductType          = packagedesc.ProductType
	ProductVersionType   = packagedesc.ProductVersionType
	SchemaProductVersion = packagedesc.SchemaProductVersion
)

func (b *Build) productEntity() platform.RecordType {
	return platform.RecordType{
		Type: ProductType, Title: "Product", Plural: "Products", Model: Product{}, Display: "title",
		Description: "A product chooses the project resources that DevOps packages into immutable versions.",
		Scope:       platform.Scope{Default: platform.ScopeTenant, Levels: map[string]string{Builder: platform.ScopeTenant, Publisher: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Publisher}, Capability: "releases"},
		Validate: func(c platform.Caller, value any) *platform.Error {
			var product *Product
			switch value := value.(type) {
			case *Product:
				product = value
			default:
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The product definition is unreadable")
			}
			if product.Namespace == "" {
				product.Namespace = product.Name
			}
			if err := product.Check(b.resolver.WithDependencies); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{reason}", err.Error())
			}
			if prior, exists := platform.Get[Product](c, product.ID); exists && prior.Project != product.Project {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A product stays in the project that owns its contents")
			}
			project, exists := platform.Get[protocol.Application](c, product.Project)
			if !exists || project.Archived || !canEditProject(c, product.Project) {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Choose a project you can edit")
			}
			for _, root := range product.Roots {
				resource, exists := platform.Get[protocol.Resource](c, protocol.ResourceID(c.Tenant, root))
				if !exists || resource.Archived || resource.Project != product.Project {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Product contents must be resources in its project")
				}
			}
			return nil
		},
	}
}

func (b *Build) productVersionEntity() platform.RecordType {
	return packagedesc.ProductVersionEntity()
}

func (b *Build) submitProductVersion(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
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
	return b.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
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
