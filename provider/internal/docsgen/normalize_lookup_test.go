// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"testing"
)

func TestFixGoLookups(t *testing.T) {
	schema := &pkgSchema{}
	if err := json.Unmarshal([]byte(`{"resources": {"docker-agent:index:UserSettings": {}},
		"functions": {"docker-agent:index:getUserSettings": {}, "docker-agent:index:getAgent": {}}}`), schema); err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"x := dockeragent.GetUserSettingsOutput(ctx, dockeragent.GetUserSettingsOutputArgs{}, nil)": "x := dockeragent.LookupUserSettingsOutput(ctx, dockeragent.LookupUserSettingsOutputArgs{}, nil)",
		"func(r dockeragent.GetUserSettingsResult) {}":                                              "func(r dockeragent.LookupUserSettingsResult) {}",
		"dockeragent.GetAgentOutput(ctx, dockeragent.GetAgentOutputArgs{})":                         "dockeragent.GetAgentOutput(ctx, dockeragent.GetAgentOutputArgs{})",
		"dockeragent.GetUserSettingsFoo":                                                            "dockeragent.GetUserSettingsFoo",
	}
	for in, want := range tests {
		if got := fixGoLookups(in, schema); got != want {
			t.Errorf("fixGoLookups(%q) = %q, want %q", in, got, want)
		}
	}
}
