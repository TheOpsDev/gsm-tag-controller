/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gcp

import (
	"maps"
	"testing"

	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
)

func TestParseNamespacedValue(t *testing.T) {
	tests := []struct {
		name           string
		namespacedName string
		orgID          string
		wantKey        string
		wantValue      string
		wantOK         bool
	}{
		{
			name:           "org-scoped binding",
			namespacedName: "123456789/environment/prod",
			orgID:          "123456789",
			wantKey:        "environment",
			wantValue:      "prod",
			wantOK:         true,
		},
		{
			name:           "different organization",
			namespacedName: "987654321/environment/prod",
			orgID:          "123456789",
			wantOK:         false,
		},
		{
			name:           "project-scoped binding",
			namespacedName: "my-project/environment/prod",
			orgID:          "123456789",
			wantOK:         false,
		},
		{
			name:           "malformed name",
			namespacedName: "environment/prod",
			orgID:          "123456789",
			wantOK:         false,
		},
		{
			name:           "empty name",
			namespacedName: "",
			orgID:          "123456789",
			wantOK:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, ok := parseNamespacedValue(tt.namespacedName, tt.orgID)
			if ok != tt.wantOK || key != tt.wantKey || value != tt.wantValue {
				t.Errorf("parseNamespacedValue(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.namespacedName, tt.orgID, key, value, ok, tt.wantKey, tt.wantValue, tt.wantOK)
			}
		})
	}
}

func TestCurrentTags(t *testing.T) {
	bindings := []*resourcemanagerpb.TagBinding{
		{TagValueNamespacedName: "123456789/environment/prod"},
		{TagValueNamespacedName: "123456789/team/payments"},
		{TagValueNamespacedName: "987654321/other-org/tag"},
		{TagValueNamespacedName: "my-project/project-scoped/tag"},
	}

	got := currentTags(bindings, "123456789")
	want := map[string]string{
		"environment": "prod",
		"team":        "payments",
	}
	if !maps.Equal(got, want) {
		t.Errorf("currentTags = %v, want %v", got, want)
	}
}
