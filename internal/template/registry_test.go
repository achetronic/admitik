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

package template_test

import (
	"strings"
	"sync"
	"testing"

	//
	"github.com/admitikcore/admitik/internal/template"
	"github.com/admitikcore/admitik/internal/template/cel"
	"github.com/admitikcore/admitik/internal/template/gotmpl"
	"github.com/admitikcore/admitik/internal/template/plain"
	"github.com/admitikcore/admitik/internal/template/starlark"
)

func mustRegistry(t *testing.T) *template.Registry {
	t.Helper()

	celExpression, err := cel.NewExpression()
	if err != nil {
		t.Fatalf("error creating the CEL engine: %v", err)
	}

	return template.NewRegistry(
		celExpression,
		cel.NewPlaceholders(celExpression),
		gotmpl.New(),
		starlark.New(),
		plain.New(),
	)
}

// testData mimics the context built from an AdmissionReview, sources included
func testData() *template.InjectedData {

	data := template.NewInjectedData()
	data.Operation = "CREATE"

	data.Object = map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   "demo",
			"labels": map[string]any{"team": "platform", "admitik.dev/mandatory-label": "yes"},
		},
	}

	data.OldObject = map[string]any{}

	data.Sources = map[int][]map[string]any{
		0: {
			{"metadata": map[string]any{"name": "first", "namespace": "default"}},
			{"metadata": map[string]any{"name": "second", "namespace": "default"}},
		},
	}

	return data
}

func TestEngines(t *testing.T) {
	tests := []struct {
		name     string
		engine   string
		source   string
		expected string
	}{
		{
			name:     "plain returns the source untouched",
			engine:   template.EnginePlain,
			source:   "apiVersion: v1\nkind: ConfigMap",
			expected: "apiVersion: v1\nkind: ConfigMap",
		},
		{
			name:     "empty engine falls back to cel",
			engine:   "",
			source:   `"name-" + object.metadata.name`,
			expected: "name-demo",
		},
		{
			name:     "cel evaluates the whole source as an expression",
			engine:   template.EngineCel,
			source:   "object.metadata.labels['team']",
			expected: "platform",
		},
		{
			name:     "cel reads the sources by integer index",
			engine:   template.EngineCel,
			source:   "sources[0][0].metadata.name",
			expected: "first",
		},
		{
			name:     "plain+cel replaces placeholders in plain text",
			engine:   template.EnginePlainWithCel,
			source:   "  value: \"{{cel: sources[0][0].metadata.name }}\"",
			expected: "  value: \"first\"",
		},
		{
			name:     "plain+cel replaces every placeholder",
			engine:   template.EnginePlainWithCel,
			source:   "{{cel: operation }}-{{cel: object.metadata.name }}",
			expected: "CREATE-demo",
		},
		{
			name:     "plain+cel replaces repeated placeholders one by one",
			engine:   template.EnginePlainWithCel,
			source:   "{{cel: object.metadata.name }} {{cel: object.metadata.name }}",
			expected: "demo demo",
		},
		{
			name:     "gotmpl renders the object",
			engine:   template.EngineGotmpl,
			source:   `{{ .object.metadata.name }}/{{ .operation }}`,
			expected: "demo/CREATE",
		},
		{
			name:     "starlark returns what the script prints",
			engine:   template.EngineStarlark,
			source:   "print(object[\"metadata\"][\"name\"])",
			expected: "demo",
		},
		{
			name:     "starlark reads the sources by integer index, like CEL does",
			engine:   template.EngineStarlark,
			source:   "print(sources[0][1][\"metadata\"][\"name\"])",
			expected: "second",
		},
		{
			name:     "starlark sees numbers as floats, as they arrive from JSON",
			engine:   template.EngineStarlark,
			source:   "print(str(1 / 2))",
			expected: "0.5",
		},
		{
			name:     "starlark encodes values with the json module",
			engine:   template.EngineStarlark,
			source:   "print(json.encode({\"team\": object[\"metadata\"][\"labels\"][\"team\"]}))",
			expected: `{"team":"platform"}`,
		},
	}

	engines := mustRegistry(t)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := engines.Evaluate(test.engine, test.source, testData())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.expected {
				t.Errorf("got %q, expected %q", got, test.expected)
			}
		})
	}
}

func TestStarlarkVarsSurviveAcrossTemplates(t *testing.T) {
	engines := mustRegistry(t)
	data := testData()

	// A condition stores something for the patch template, like the samples do
	_, err := engines.Evaluate(template.EngineStarlark, `
vars.update({"existing_route": object["metadata"]["name"]})
print("ok")
`, data)
	if err != nil {
		t.Fatalf("unexpected error in the condition: %v", err)
	}

	got, err := engines.Evaluate(template.EngineStarlark, `
print(vars.get("existing_route", "missing"))
`, data)
	if err != nil {
		t.Fatalf("unexpected error in the patch: %v", err)
	}

	if got != "demo" {
		t.Errorf("got %q, expected %q", got, "demo")
	}
}

func TestGotmplSetVarSurvivesAcrossTemplates(t *testing.T) {
	engines := mustRegistry(t)
	data := testData()

	if _, err := engines.Evaluate(template.EngineGotmpl, `{{ setVar "computed" "value" }}`, data); err != nil {
		t.Fatalf("unexpected error in the condition: %v", err)
	}

	got, err := engines.Evaluate(template.EngineGotmpl, `{{ .vars.computed }}`, data)
	if err != nil {
		t.Fatalf("unexpected error in the patch: %v", err)
	}

	if got != "value" {
		t.Errorf("got %q, expected %q", got, "value")
	}
}

func TestStarlarkCannotMutateTheSources(t *testing.T) {
	engines := mustRegistry(t)

	_, err := engines.Evaluate(template.EngineStarlark, `
sources[0].append({})
`, testData())

	if err == nil {
		t.Fatal("expected an error when mutating a frozen source, got nil")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("got error %q, expected a 'frozen' error", err.Error())
	}
}

func TestUnknownEngineIsReported(t *testing.T) {
	engines := mustRegistry(t)

	_, err := engines.Evaluate("nope", "whatever", testData())

	if err == nil {
		t.Fatal("expected an error for an unknown engine, got nil")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("got error %q, expected it to name the engine", err.Error())
	}
}

func TestRegistryNames(t *testing.T) {
	expected := []string{"cel", "gotmpl", "plain", "plain+cel", "starlark"}

	got := mustRegistry(t).Names()
	if len(got) != len(expected) {
		t.Fatalf("got %v, expected %v", got, expected)
	}

	for index, name := range expected {
		if got[index] != name {
			t.Errorf("got %v, expected %v", got, expected)
			break
		}
	}
}

func TestEnginesAreSafeUnderConcurrentUse(t *testing.T) {
	engines := mustRegistry(t)

	cases := map[string]string{
		template.EngineCel:          "object.metadata.name",
		template.EnginePlainWithCel: `name: {{cel: object.metadata.name }}`,
		template.EngineGotmpl:       `{{ .object.metadata.name }}`,
		template.EngineStarlark:     `print(object["metadata"]["name"])`,
	}

	var waitGroup sync.WaitGroup
	for index := 0; index < 8; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for run := 0; run < 50; run++ {
				for engine, source := range cases {
					if _, err := engines.Evaluate(engine, source, testData()); err != nil {
						t.Errorf("engine %s failed: %v", engine, err)
					}
				}
			}
		}()
	}

	waitGroup.Wait()
}
