/*
Copyright 2024.

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

package common

import (
	"testing"
)

func TestResourceLabel(t *testing.T) {
	cases := map[string]struct {
		group    string
		version  string
		resource string
		expected string
	}{
		"a grouped resource keeps the group": {
			group: "apps", version: "v1", resource: "deployments", expected: "apps/v1/deployments",
		},
		"a core resource has no leading slash": {
			group: "", version: "v1", resource: "pods", expected: "v1/pods",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			got := ResourceLabel(testCase.group, testCase.version, testCase.resource)
			if got != testCase.expected {
				t.Errorf("got %q, expected %q", got, testCase.expected)
			}
		})
	}
}
