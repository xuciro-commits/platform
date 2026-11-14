package idp

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type Startup struct {
	Profile, Data, KeyFile, Mint string
	NewKey                       bool
	TokenTTL                     time.Duration
}

func (s Startup) KeyPath() string {
	if s.KeyFile != "" {
		return s.KeyFile
	}
	return filepath.Join(s.Data, "idp.key")
}

func (s Startup) Run(now time.Time) (signer *Local, done bool, err error) {
	if s.NewKey {
		if err := MakeKeyFile(s.KeyPath()); err != nil {
			return nil, false, err
		}
		log.Printf("made the signing key %s", s.KeyPath())
		return nil, true, nil
	}
	if s.Profile == "lightweight" {
		if signer, err = LoadLocal(s.KeyPath()); err != nil {
			return nil, false, err
		}
	}
	if s.Mint == "" {
		return signer, false, nil
	}
	if signer == nil {
		return nil, false, fmt.Errorf("-mint-token is the lightweight profile's; a delivery host signs in at its OpenID provider")
	}
	token, err := signer.Mint(s.Mint, s.TokenTTL, now)
	if err != nil {
		return nil, false, err
	}
	fmt.Println(token)
	return signer, true, nil
}

type Flags struct {
	Addr, Database, Issuer, Keys, Web string
	IdentityProxy                     string
	HostAdmins                        string
	TenantsFile, TemplatesDir         string
	Release                           int
	ReleaseDigest                     string
	Files                             string
	Project                           bool
	SnapshotEvery                     int64
	Profile                           string
	Data                              string
	IDPKey                            string
	Mint                              string
	NewKey                            bool
	TokenTTL                          time.Duration
	Packages                          string
}

func (f *Flags) Register(addr string) {
	flag.StringVar(&f.Addr, "addr", addr, "listen address")
	flag.StringVar(&f.Database, "database", "", "PostgreSQL URL of the journal (empty: memory only)")
	flag.StringVar(&f.Issuer, "oidc-issuer", "", "OpenID issuer whose access tokens are accepted (empty: development tokens, the token is the subject)")
	flag.StringVar(&f.HostAdmins, "host-admins", "", "comma-separated authenticated subjects allowed to administer this host; empty: no host administrators")
	flag.StringVar(&f.Keys, "oidc-keys", "", "JWKS URL of the issuer, when the server reaches it on another address")
	flag.StringVar(&f.IdentityProxy, "oidc-proxy", "", "fixed identity service upstream exposed at /auth on this host")
	flag.StringVar(&f.Files, "files", "", "S3-compatible store of file bytes, http(s)://host:port/bucket, keys from PLATFORM_S3_ACCESS_KEY and PLATFORM_S3_SECRET_KEY (empty: memory)")
	flag.StringVar(&f.TenantsFile, "tenants", "", "JSON file with the host's tenants, their seats and starting settings; the host console appends to it (empty: the built-in development tenant)")
	flag.StringVar(&f.TemplatesDir, "templates", "", "directory of tenant templates the host console offers (empty: none)")
	flag.BoolVar(&f.Project, "project", false, "copy records into PostgreSQL tables per tenant for tools outside the host (needs -database)")
	flag.Int64Var(&f.SnapshotEvery, "snapshot-every", 10000, "save each tenant's state every this many journal entries and at shutdown, and start from the newest compatible snapshot (0: replay the whole journal)")
	flag.StringVar(&f.Web, "web", "", "directory of the workspace's build, served at / (empty: API only)")
	flag.StringVar(&f.Profile, "profile", "delivery", "delivery: PostgreSQL journal, S3 file bytes, external OpenID provider; lightweight: one -data directory, an internal signing key, no external services")
	flag.StringVar(&f.Data, "data", "", "directory of a lightweight host's state, journals and file bytes (required with -profile lightweight)")
	flag.StringVar(&f.IDPKey, "idp-key", "", "file with the lightweight host's token signing key (default: <data>/idp.key)")
	flag.BoolVar(&f.NewKey, "idp-new-key", false, "make a signing key when none exists, refuse to replace one, and stop")
	flag.StringVar(&f.Mint, "mint-token", "", "print an access token for this subject and stop (<user:email> or <client:id>, as the seats name them)")
	flag.DurationVar(&f.TokenTTL, "token-ttl", 12*time.Hour, "how long a minted token is accepted")
	flag.StringVar(&f.Packages, "packages", os.Getenv("PLATFORM_PACKAGES_DIR"), "directory of package descriptors the console offers (empty: no packages)")
	flag.IntVar(&f.Release, "release", 0, "the system release this binary serves (0: a development build)")
	flag.StringVar(&f.ReleaseDigest, "release-digest", "", "sha256 of this binary as the system release recorded it")
}

func (f *Flags) Validate() (err error) {
	f.Profile, err = Profile{Name: f.Profile, Data: f.Data, KeyFile: f.IDPKey, Database: f.Database, Files: f.Files,
		Issuer: f.Issuer, Keys: f.Keys, Proxy: f.IdentityProxy, Project: f.Project}.Check()
	return err
}

func (f *Flags) Startup() Startup {
	return Startup{Profile: f.Profile, Data: f.Data, KeyFile: f.IDPKey, Mint: f.Mint, NewKey: f.NewKey, TokenTTL: f.TokenTTL}
}
