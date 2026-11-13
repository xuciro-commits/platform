package releases

import (
	"encoding/json"
	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type BootstrapStage struct {
	Tenant          string
	Candidate       platform.ReleaseCandidate
	Context         platform.AssetReleaseContext
	Request         *OwnerDraftRequest
	IncludeArchived bool
	Registry        *platform.AssetRegistry
	Drafts          []platform.AssetDraft
	Put             func(app string, receipt *pb.ChangeRecord, value any) *kernel.Error
	MediaVersions   func(plan platform.AssetInstallation) ([]platform.MediaSetVersion, error)
	KeepChanges     func(receipt *pb.ChangeRecord)
	Register        func(plan platform.AssetInstallation) error
	Entity          func(typ, name string) (info platform.EntityInfo, hasRow, ok bool)
	Matches         func(typ, name string, reopen, managed bool) (int, error)
	Decide          func(info platform.EntityInfo, draft platform.AssetDraft, submission *pb.Submission) (*pb.ChangeRecord, error)
	Rebind          func(app string) (*platform.AssetRegistry, error)
	AttachHosts     func()
}

func (s BootstrapStage) Run() (*pb.ChangeRecord, error) {
	registry, context, candidate, request := s.Registry, s.Context, s.Candidate, s.Request
	var receipt *pb.ChangeRecord
	declare := func(asset platform.ReleaseAsset) error {
		if context.DraftsOnly && asset.Ref.Kind != platform.AssetMediaSet {
			return nil
		}
		plans, err := registry.PrepareReleaseAssets([]platform.ReleaseAsset{asset}, context)
		if err != nil {
			return err
		}
		for _, plan := range plans {
			if context.DraftsOnly && plan.Ref.Kind == platform.AssetMediaSet {
				var set platform.MediaSet
				if err := json.Unmarshal(plan.Image, &set); err != nil {
					return err
				}
				if refusal := s.Put(plan.Ref.App, receipt, set); refusal != nil {
					return refusal
				}
				images, err := s.MediaVersions(plan)
				if err != nil {
					return err
				}
				for _, snapshot := range images {
					if refusal := s.Put(plan.Ref.App, receipt, snapshot); refusal != nil {
						return refusal
					}
				}
				s.KeepChanges(receipt)
			}
			if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{Private: true}); err != nil {
				return err
			}
			if context.DraftsOnly {
				continue
			}
			if err := s.Register(plan); err != nil {
				return err
			}
		}
		return nil
	}
	assets := map[platform.AssetRef]platform.ReleaseAsset{}
	hasDraft := map[platform.AssetRef]bool{}
	for _, draft := range s.Drafts {
		hasDraft[draft.Ref] = true
	}
	for _, asset := range candidate.Assets {
		assets[asset.Ref] = asset
		if context.ManagedAssets != nil && !context.ManagedAssets[asset.Ref] {
			continue
		}
		if !hasDraft[asset.Ref] && !context.DraftsOnly {
			if err := declare(asset); err != nil {
				return nil, err
			}
		}
	}
	for ordinal, draft := range s.Drafts {
		info, hasRow, ok := s.Entity(draft.Type, draft.Name)
		if !ok {
			return nil, fmt.Errorf("%s does not host %s", s.Tenant, draft.Type)
		}
		matches, err := s.Matches(draft.Type, DraftLogicalName(draft.Ref), s.IncludeArchived, context.ManagedBy != "")
		if err != nil {
			return nil, err
		}
		if matches > 1 {
			return nil, fmt.Errorf("asset draft %s/%s is ambiguous", draft.Type, draft.Name)
		}
		if request != nil && (matches > 0 || hasRow) {
			return nil, fmt.Errorf("asset %s already has a draft; open that draft instead", draft.Ref)
		}
		if matches == 0 {
			submission, err := DraftSubmission(s.Tenant, candidate.ID, context, request, ordinal, draft, info)
			if err != nil {
				return nil, err
			}
			if receipt, err = s.Decide(info, draft, submission); err != nil {
				return nil, err
			}
		}
		if registry, err = s.Rebind(info.App); err != nil {
			return nil, err
		}
		if err := declare(assets[draft.Ref]); err != nil {
			return nil, err
		}
		s.AttachHosts()
	}
	return receipt, nil
}
