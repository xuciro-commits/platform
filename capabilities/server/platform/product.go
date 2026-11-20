package platform

import "time"

type ProductVersionRequest struct {
	ID, Version, Title, Namespace, Project string
	Roots                                  []AssetRef
	Requires                               []string
	Candidate                              string
	At                                     time.Time
}

type FrozenProductVersion struct {
	Version, Candidate, CandidateDigest, ContentDigest string
	Descriptor                                         PackageDescriptor
	At                                                 time.Time
}

type ProductVersionView struct {
	Product, Version, Candidate string
	Descriptor                  PackageDescriptor
}
