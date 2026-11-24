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
	"runtime"
	"sync"
	"testing"
	"time"

	//
	starletconv "github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"

	//
	"github.com/admitikcore/admitik/internal/template"
)

// concurrentEvaluations is how many admission requests are simulated at the same time
const concurrentEvaluations = 8

func megabytes(bytes uint64) string {
	return fmt.Sprintf("%.1f MiB", float64(bytes)/(1024*1024))
}

func heapAllocated() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	return stats.HeapAlloc
}

func totalAllocated() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	return stats.TotalAlloc
}

// liveHeap returns the heap held by live objects after a collection
func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()

	return heapAllocated()
}

// measurement holds what an operation cost in memory
type measurement struct {
	peakAllocated uint64 // el pico de memoria viva durante la operacion
	totalChurn    uint64 // memoria total reservada, incluida la que se tira
	elapsed       time.Duration
}

// measure runs the operation while sampling the live heap, so the peak is a real peak
// and not the value left when it finishes
func measure(operation func()) measurement {

	baseline := liveHeap()
	churnBefore := totalAllocated()

	stop := make(chan struct{})
	peak := baseline
	sampled := make(chan struct{})

	go func() {
		defer close(sampled)
		for {
			select {
			case <-stop:
				return
			default:
				if current := heapAllocated(); current > peak {
					peak = current
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	started := time.Now()
	operation()
	elapsed := time.Since(started)
	close(stop)
	<-sampled

	return measurement{
		peakAllocated: peak - baseline,
		totalChurn:    totalAllocated() - churnBefore,
		elapsed:       elapsed,
	}
}

func (m measurement) String() string {
	return fmt.Sprintf("pico %s, reservado %s, %s",
		megabytes(m.peakAllocated), megabytes(m.totalChurn), m.elapsed.Round(time.Millisecond))
}

// configMaps builds a context with many plain objects
func configMaps(count int) *template.InjectedData {

	objects := make([]map[string]any, 0, count)
	for index := 0; index < count; index++ {
		objects = append(objects, map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      fmt.Sprintf("config-%06d", index),
				"namespace": "default",
				"labels": map[string]any{
					"app.kubernetes.io/name":      "demo",
					"app.kubernetes.io/instance":  fmt.Sprintf("instance-%d", index%50),
					"app.kubernetes.io/component": "backend",
					"app.kubernetes.io/part-of":   "platform",
				},
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": `{"apiVersion":"v1"}`,
					"meta.helm.sh/release-name":                        "demo",
					"meta.helm.sh/release-namespace":                   "default",
				},
			},
			"data": map[string]any{
				"config.yaml": "server:\n  port: 8080\n  timeout: 30s\nlog:\n  level: info\n",
				"extra.json":  `{"retries":3,"backoff":"1s"}`,
				"index":       float64(index),
			},
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

// httpRoutes builds the corpus of the cross-check policies: many Gateway API routes, each one
// close to a real HTTPRoute
func httpRoutes(count int) *template.InjectedData {

	routes := make([]map[string]any, 0, count)
	for index := 0; index < count; index++ {
		routes = append(routes, map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata": map[string]any{
				"name":      fmt.Sprintf("route-%06d", index),
				"namespace": fmt.Sprintf("team-%d", index%40),
				"labels": map[string]any{
					"app.kubernetes.io/name":     "gateway",
					"app.kubernetes.io/instance": fmt.Sprintf("gw-%d", index%20),
				},
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": `{"apiVersion":"gateway.networking.k8s.io/v1"}`,
				},
			},
			"spec": map[string]any{
				"parentRefs": []any{
					map[string]any{"name": fmt.Sprintf("gateway-%d", index%5), "namespace": "infra", "sectionName": "https"},
				},
				"hostnames": []any{
					fmt.Sprintf("api-%d.example.com", index%100),
					fmt.Sprintf("www-%d.example.com", index%100),
				},
				"rules": []any{
					map[string]any{
						"matches": []any{
							map[string]any{
								"path": map[string]any{"type": "PathPrefix", "value": fmt.Sprintf("/v%d/users", index%30)},
								"headers": []any{
									map[string]any{"name": "x-tenant", "value": fmt.Sprintf("tenant-%d", index%10)},
								},
							},
						},
						"backendRefs": []any{
							map[string]any{"name": fmt.Sprintf("svc-%d", index%50), "port": float64(8080)},
						},
					},
				},
			},
		})
	}

	data := template.NewInjectedData()
	data.Operation = "CREATE"
	data.Object = routes[0]
	data.Sources = map[int][]map[string]any{0: routes}

	return data
}

// contextCopy returns a copy of the context, as every request builds its own
func contextCopy(data *template.InjectedData) *template.InjectedData {
	local := *data
	local.Vars = map[string]any{}

	return &local
}

// TestConversionMemoryAtScale compares building the Starlark tree through JSON, which is what the
// engine used to do, with building it directly
func TestConversionMemoryAtScale(t *testing.T) {

	if testing.Short() {
		t.Skip("medición de memoria: se omite con -short")
	}

	const objects = 12000

	values := configMaps(objects).ToMap()

	t.Logf("contexto de %d objetos: %s en Go", objects, megabytes(liveHeap()))

	var throughJSON []starlark.Value
	jsonMeasurement := measure(func() {
		throughJSON = make([]starlark.Value, 0, len(values))
		for _, value := range values {
			converted, err := starletconv.GoToStarlarkViaJSON(value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			throughJSON = append(throughJSON, converted)
		}
	})
	runtime.KeepAlive(throughJSON)
	throughJSON = nil

	t.Logf("json   : %s", jsonMeasurement)

	var direct []starlark.Value
	directMeasurement := measure(func() {
		direct = make([]starlark.Value, 0, len(values))
		for _, value := range values {
			converted, err := toStarlark(value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			direct = append(direct, converted)
		}
	})
	runtime.KeepAlive(direct)
	direct = nil

	t.Logf("directo: %s", directMeasurement)

	if directMeasurement.peakAllocated >= jsonMeasurement.peakAllocated {
		t.Errorf("the direct conversion peaks at %s, the JSON one at %s",
			megabytes(directMeasurement.peakAllocated), megabytes(jsonMeasurement.peakAllocated))
	}
}

// TestConcurrentEvaluationMemoryAtScale measures the case that hurts in production: several
// admission requests evaluated at the same time, with and without the reused tree
func TestConcurrentEvaluationMemoryAtScale(t *testing.T) {

	if testing.Short() {
		t.Skip("medición de memoria: se omite con -short")
	}

	const objects = 12000

	engine := New()
	source := `
def run():
    total = 0
    for item in sources[0]:
        total = total + len(item["metadata"]["labels"])
    print(str(total))

run()
`

	data := configMaps(objects)

	rebuilding := measure(func() {
		evaluateConcurrently(t, engine, source, data, "")
	})
	t.Logf("%d evaluaciones a la vez, arbol nuevo en cada una: %s", concurrentEvaluations, rebuilding)

	// Warm up the cache so the measurement is of the reuse and not of the first build
	if _, err := engine.Evaluate(source, withVersion(contextCopy(data), "v1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reusing := measure(func() {
		evaluateConcurrently(t, engine, source, data, "v1")
	})
	t.Logf("%d evaluaciones a la vez, arbol reutilizado: %s", concurrentEvaluations, reusing)

	if reusing.peakAllocated*4 > rebuilding.peakAllocated {
		t.Errorf("reusing the tree saved less than expected: %s against %s",
			megabytes(reusing.peakAllocated), megabytes(rebuilding.peakAllocated))
	}
}

// TestHTTPRouteCrossCheckMemoryAtScale reproduces the expensive case: crossing thousands of
// routes to find duplicates, which is what the routes sample does
func TestHTTPRouteCrossCheckMemoryAtScale(t *testing.T) {

	if testing.Short() {
		t.Skip("medición de memoria: se omite con -short")
	}

	const objects = 12000

	engine := New()

	// Same shape as the sample: compare the incoming route against every existing one
	source := `
def has_matching_parent_ref(incoming_refs, existing_refs):
    for inc_ref in incoming_refs:
        inc_name = inc_ref["name"]
        inc_namespace = inc_ref.get("namespace")
        for ex_ref in existing_refs:
            if inc_name == ex_ref["name"] and inc_namespace == ex_ref.get("namespace"):
                return True
    return False

def path_conflict_exists(incoming_route, existing_route):
    for inc_rule in incoming_route["spec"]["rules"]:
        for inc_match in inc_rule["matches"]:
            inc_path_type = inc_match["path"]["type"]
            if inc_path_type not in ["Exact", "PathPrefix"]:
                continue
            inc_path = inc_match["path"]["value"].rstrip("/")
            for ex_rule in existing_route["spec"]["rules"]:
                for ex_match in ex_rule["matches"]:
                    if ex_match["path"]["type"] != inc_path_type:
                        continue
                    if inc_path == ex_match["path"]["value"].rstrip("/"):
                        return True
    return False

def compute_raw_match():
    incoming_route = object
    existing_routes = sources[0]

    incoming_name = incoming_route["metadata"]["name"]
    incoming_namespace = incoming_route["metadata"]["namespace"]
    incoming_parent_refs = incoming_route["spec"]["parentRefs"]
    incoming_hostnames = incoming_route["spec"]["hostnames"]

    for existing_route in existing_routes:
        if incoming_name == existing_route["metadata"]["name"] and incoming_namespace == existing_route["metadata"]["namespace"]:
            continue
        if not has_matching_parent_ref(incoming_parent_refs, existing_route["spec"]["parentRefs"]):
            continue
        matching_hostnames = [h for h in incoming_hostnames if h in existing_route["spec"]["hostnames"]]
        if not matching_hostnames:
            continue
        if path_conflict_exists(incoming_route, existing_route):
            vars["existing_route"] = {
                "name": existing_route["metadata"]["name"],
                "namespace": existing_route["metadata"]["namespace"],
            }
            break

    print("ignore-condition")

compute_raw_match()
`

	data := httpRoutes(objects)
	t.Logf("corpus de %d HTTPRoutes: %s en Go", objects, megabytes(liveHeap()))

	single := measure(func() {
		if _, err := engine.Evaluate(source, contextCopy(data)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Logf("una evaluación, arbol nuevo: %s", single)

	// What the engine did before the direct conversion, for reference
	values := data.ToMap()
	var throughJSON []starlark.Value
	jsonConversion := measure(func() {
		throughJSON = make([]starlark.Value, 0, len(values))
		for _, value := range values {
			converted, err := starletconv.GoToStarlarkViaJSON(value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			throughJSON = append(throughJSON, converted)
		}
	})
	runtime.KeepAlive(throughJSON)
	throughJSON = nil
	t.Logf("solo construir el arbol por json: %s", jsonConversion)

	// Warm up the cache, then measure one evaluation reusing the tree
	if _, err := engine.Evaluate(source, withVersion(contextCopy(data), "routes-v1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reused := measure(func() {
		if _, err := engine.Evaluate(source, withVersion(contextCopy(data), "routes-v1")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Logf("una evaluación, arbol reutilizado: %s", reused)

	rebuilding := measure(func() {
		evaluateConcurrently(t, engine, source, data, "")
	})
	t.Logf("%d evaluaciones a la vez, arbol nuevo en cada una: %s", concurrentEvaluations, rebuilding)

	reusing := measure(func() {
		evaluateConcurrently(t, engine, source, data, "routes-v1")
	})
	t.Logf("%d evaluaciones a la vez, arbol reutilizado: %s", concurrentEvaluations, reusing)

	if reusing.peakAllocated*4 > rebuilding.peakAllocated {
		t.Errorf("reusing the tree saved less than expected: %s against %s",
			megabytes(reusing.peakAllocated), megabytes(rebuilding.peakAllocated))
	}
}

func withVersion(data *template.InjectedData, version string) *template.InjectedData {
	data.SourcesVersion = version

	return data
}

// evaluateConcurrently runs 'concurrentEvaluations' evaluations at the same time
func evaluateConcurrently(t *testing.T, engine *Engine, source string, data *template.InjectedData, version string) {
	t.Helper()

	var waitGroup sync.WaitGroup
	for worker := 0; worker < concurrentEvaluations; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			local := contextCopy(data)
			local.SourcesVersion = version

			if _, err := engine.Evaluate(source, local); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	waitGroup.Wait()
}
