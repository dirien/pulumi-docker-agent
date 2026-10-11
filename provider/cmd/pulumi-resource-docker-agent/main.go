// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Command pulumi-resource-docker-agent runs the Docker Agent Pulumi provider.
package main

import (
	"context"
	"fmt"
	"os"

	version "github.com/dirien/pulumi-docker-agent/provider"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/provider"
)

func main() {
	if err := provider.Provider().Run(context.Background(), provider.Name, version.Version); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s", err.Error())
		os.Exit(1)
	}
}
