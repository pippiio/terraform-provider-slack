//go:build tools

// Package tools pins the build-time tooling this module depends on.
//
// The blank import is load-bearing: without it nothing in the module references
// terraform-plugin-docs, so `go mod tidy` drops it from go.mod and `go generate`
// then fails with "no required module provides package". The `tools` build tag keeps
// the package out of ordinary builds.
//
// See https://go.dev/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)

// Format Terraform code for use in documentation.
//go:generate terraform fmt -recursive ../examples/

// Generate documentation.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir .. -provider-name slack
