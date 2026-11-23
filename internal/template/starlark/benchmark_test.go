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

package starlark

import (
	"fmt"
	"testing"

	//
	starletconv "github.com/1set/starlet/dataconv"

	//
	"github.com/freepik-company/admitik/internal/template"
)

// contextWithObjects builds a context holding 'count' source objects, similar to a policy
// listing configmaps across a cluster
func contextWithObjects(count int) *template.InjectedData {

	objects := make([]map[string]any, 0, count)
	for index := 0; index < count; index++ {
		objects = append(objects, map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      fmt.Sprintf("config-%d", index),
				"namespace": "default",
				"labels":    map[string]any{"app": "demo", "tier": "backend"},
			},
			"data": map[string]any{"key": "value", "index": float64(index)},
		})
	}

	data := template.NewInjectedData()
	data.Operation = "CREATE"
	data.Object = map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": "demo"},
	}
	data.Sources = map[int][]map[string]any{0: objects}

	return data
}

// BenchmarkContextConversion compares turning the context into Starlark values through JSON,
// which is what the engine used to do, with the direct conversion
func BenchmarkContextConversion(b *testing.B) {

	values := contextWithObjects(2000).ToMap()

	b.Run("json", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, value := range values {
				if _, err := starletconv.GoToStarlarkViaJSON(value); err != nil {
					b.Fatalf("unexpected error: %v", err)
				}
			}
		}
	})

	b.Run("direct", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, value := range values {
				if _, err := toStarlark(value); err != nil {
					b.Fatalf("unexpected error: %v", err)
				}
			}
		}
	})
}

// BenchmarkEvaluate measures a full evaluation with a big context, which is the cost paid on
// every admission request
func BenchmarkEvaluate(b *testing.B) {

	engine := New()
	// Top level loops are not allowed by the interpreter, so the script is wrapped like the samples do
	source := `
def run():
    for item in sources["0"]:
        name = item["metadata"]["name"]
    print(len(sources["0"]))

run()
`

	for _, count := range []int{10, 1000} {
		b.Run(fmt.Sprintf("objects=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := engine.Evaluate(source, contextWithObjects(count)); err != nil {
					b.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
