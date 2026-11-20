//go:build !embedweb

package main

import platformserver "platformserver/host"

func configureStandalone(*platformserver.Deployment, *string) {}
