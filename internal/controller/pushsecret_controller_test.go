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

package controller

import (
	"maps"
	"testing"
)

func TestParseTagsAnnotation(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "simple pairs",
			raw:  "environment=prod,team=payments",
			want: map[string]string{"environment": "prod", "team": "payments"},
		},
		{
			name: "whitespace around pairs is trimmed",
			raw:  "environment=prod, team=payments, namespace=ds-back, foo=bar",
			want: map[string]string{"environment": "prod", "team": "payments", "namespace": "ds-back", "foo": "bar"},
		},
		{
			name:    "missing value",
			raw:     "environment=",
			wantErr: true,
		},
		{
			name:    "missing separator",
			raw:     "environment",
			wantErr: true,
		},
		{
			name:    "empty annotation",
			raw:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTagsAnnotation(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseTagsAnnotation(%q) = %v, want error", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTagsAnnotation(%q) returned error: %v", tt.raw, err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("parseTagsAnnotation(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
