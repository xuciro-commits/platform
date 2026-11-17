package descriptor

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type TestPlanEnvironment[F any] struct {
	App, Type, Builder string
	Caller             func() platform.Caller
	Authority          func() platform.Authority
	SourceVersion      func() string
	WithDependencies   func(platform.ReleaseAsset) (platform.ReleaseAsset, error)
	CheckDependencies  func(platform.ReleaseAsset) error
	EqualAsset         func(platform.ReleaseAsset, platform.ReleaseAsset) bool
	Decode             func([]byte, any) error
	JointDeclarations  func() map[platform.AssetRef]platform.ReleaseAsset
	FunctionDefinition func(string, int) (platform.AIFunction, int, bool)
	Functions          func() ([]F, error)
	DraftID            func(platform.AssetRef) string
}

type EvaluationPublication[O, P, F any] struct {
	Asset func(TestPlan[O, P, F], int) (platform.ReleaseAsset, error)
	Draft func(platform.ReleaseAsset) (TestPlan[O, P, F], error)
}

type TestPlanPublication[O, P, F any] struct {
	Asset          func(TestPlan[O, P, F], int) (platform.ReleaseAsset, error)
	Draft          func(platform.ReleaseAsset) (TestPlan[O, P, F], error)
	Decode         func(json.RawMessage) (platform.AssetRef, error)
	Validate       func(json.RawMessage) error
	ReadFrozen     func(json.RawMessage) (json.RawMessage, error)
	Assets         func() ([]platform.ReleaseAsset, error)
	Resolve        func(platform.AssetRef, string) (platform.ReleaseAsset, error)
	FreezeBatch    func([]platform.ReleaseAsset) ([]platform.ReleasePublication, error)
	VersionedDraft func(string, func() ([]platform.ReleaseAsset, error)) ([]platform.ReleaseAsset, []platform.ReleaseAsset, platform.AssetRef, platform.AssetRef, bool, error)
	Submit         func(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)
}
