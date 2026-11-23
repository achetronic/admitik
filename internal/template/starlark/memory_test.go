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
	"github.com/freepik-company/admitik/internal/template"
)

// largeContext builds a context resembling a policy that lists configmaps across a cluster.
// Every object is close to a real ConfigMap: labels, annotations and some data
func largeContext(count int) *template.InjectedData {

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
	liveAfter     uint64 // memoria retenida al terminar
	elapsed       time.Duration
}

// measure runs the operation while sampling the live heap, so the peak is a real peak
// and not the value left when it finishes
func measure(operation func()) measurement {

	baseline := liveHeap()
	churnBefore := totalAllocated()

	stop := make(chan struct{})
	peak := uint64(0)
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

	churn := totalAllocated() - churnBefore
	live := liveHeap()

	if peak < baseline {
		peak = baseline
	}

	return measurement{
		peakAllocated: peak - baseline,
		totalChurn:    churn,
		liveAfter:     live - min(live, baseline),
		elapsed:       elapsed,
	}
}

func (m measurement) String() string {
	return fmt.Sprintf("pico %s, reservado %s, retenido %s, %s",
		megabytes(m.peakAllocated), megabytes(m.totalChurn), megabytes(m.liveAfter), m.elapsed.Round(time.Millisecond))
}

// TestConversionMemoryAtScale compares building the Starlark tree through JSON, which is what the
// engine used to do, with building it directly
func TestConversionMemoryAtScale(t *testing.T) {

	if testing.Short() {
		t.Skip("medicion de memoria: se omite con -short")
	}

	const objects = 12000

	data := largeContext(objects)
	values := data.ToMap()

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
		for _, value := range throughJSON {
			value.Freeze()
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
		for _, value := range direct {
			value.Freeze()
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
// admission requests being evaluated at the same time, each one rebuilding the tree
func TestConcurrentEvaluationMemoryAtScale(t *testing.T) {

	if testing.Short() {
		t.Skip("medicion de memoria: se omite con -short")
	}

	const objects = 12000
	const concurrency = 8

	engine := New()
	source := `
def run():
    total = 0
    for item in sources[0]:
        total = total + len(item["metadata"]["labels"])
    print(str(total))

run()
`

	data := largeContext(objects)

	measurementWithNewTree := measure(func() {
		var waitGroup sync.WaitGroup
		for worker := 0; worker < concurrency; worker++ {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()

				// Cada evaluacion escribe en su propio contexto, como en un request real
				local := *data
				local.Vars = map[string]any{}

				if _, err := engine.Evaluate(source, &local); err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		waitGroup.Wait()
	})

	t.Logf("%d evaluaciones a la vez, arbol nuevo en cada una: %s", concurrency, measurementWithNewTree)

	// The same, but with the tree built once and shared. This is what caching by cache
	// version would give, and it is the target of the next phase
	values := data.ToMap()
	predeclared, err := predeclaredValues(values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	program, err := engine.programs.get(source, contextKeys(values), sortedNames(predeclared))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	measurementWithSharedTree := measure(func() {
		var waitGroup sync.WaitGroup
		for worker := 0; worker < concurrency; worker++ {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				thread := &starlark.Thread{Name: "template", Print: func(_ *starlark.Thread, _ string) {}}
				if _, err := program.Init(thread, predeclared); err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		waitGroup.Wait()
	})

	t.Logf("%d evaluaciones a la vez, arbol compartido y congelado: %s", concurrency, measurementWithSharedTree)

	// Guard for the cached tree work: rebuilding it per evaluation must stay far more expensive
	if measurementWithSharedTree.peakAllocated*4 > measurementWithNewTree.peakAllocated {
		t.Errorf("sharing the tree saved less than expected: %s against %s",
			megabytes(measurementWithSharedTree.peakAllocated), megabytes(measurementWithNewTree.peakAllocated))
	}

	runtime.KeepAlive(predeclared)
}
