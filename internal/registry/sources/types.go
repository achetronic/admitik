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

package sources

import (
	"sync"

	//
	"k8s.io/apimachinery/pkg/runtime/schema"

	//
	"github.com/freepik-company/admitik/internal/informer"
)

// SourcesRegistry manage sources watchers' lifecycle
type SourcesRegistry struct {
	mu sync.RWMutex

	informers map[schema.GroupVersionResource]*informer.Informer

	// versions is increased every time an informer reports a change, so callers can tell
	// whether the cached objects of a resource are the same ones they saw before
	versions map[schema.GroupVersionResource]uint64
}

// Snapshot is the cached content of a resource type at a given version.
// Two snapshots with the same version hold the same objects
type Snapshot struct {
	Objects []*map[string]any
	Version uint64
}
