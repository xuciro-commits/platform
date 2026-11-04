package platform

import "time"

type ProductVersionRequest struct {
	ID        string
	Version   string
	Title     string
	Namespace string
	Project   string
	Roots     []AssetRef
	Requires  []string
	Candidate string
	At        time.Time
}

type FrozenProductVersion struct {
	Version         string
	Candidate       string
	CandidateDigest string
	ContentDigest   string
	Descriptor      PackageDescriptor
	At              time.Time
}

type ProductVersionView struct {
	Product    string
	Version    string
	Candidate  string
	Descriptor PackageDescriptor
}
