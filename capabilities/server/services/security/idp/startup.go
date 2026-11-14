package idp

import (
	"fmt"
	"log"
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
