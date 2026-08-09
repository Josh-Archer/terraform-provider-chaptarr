package main

import (
	"context"
	"flag"
	"log"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with debugger support")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.opentofu.org/josh-archer/chaptarr",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
