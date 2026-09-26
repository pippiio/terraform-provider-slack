package main

import (
	"context"
	"flag"
	"log"
	"terraform-provider-slack/internal/provider"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var (
	// these will be set by the goreleaser configuration
	// to appropriate values for the compiled binary.
	version string = "dev"

	// goreleaser can pass other information to the main package, such as the specific commit
	// https://goreleaser.com/cookbooks/using-main.version/
)

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		// NOTE: this is a development provider address, not a Terraform Registry
		// one. Consuming it requires a dev_overrides block or a filesystem mirror
		// in the Terraform CLI configuration; it will not resolve from the public
		// registry. Publishing to the Registry requires changing this to a
		// registry.terraform.io/<namespace>/slack address.
		Address: "pippiio.com/pippiio/slack",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)

	if err != nil {
		log.Fatal(err.Error())
	}
}
