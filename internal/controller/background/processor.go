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

package background

import (
	"context"

	//
	"k8s.io/apimachinery/pkg/watch"
)

// Processor reacts to the events of a watched resource. The background controller only knows when
// something happened; what to do with it belongs to the processor
type Processor interface {
	Process(ctx context.Context, resourceType string, eventType watch.EventType, object ...map[string]any)
}
