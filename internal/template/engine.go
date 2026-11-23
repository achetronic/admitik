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

package template

import (
	"fmt"
	"slices"
	"strings"
)

const (
	EngineCel          string = "cel"
	EngineGotmpl       string = "gotmpl"
	EnginePlain        string = "plain"
	EngineStarlark     string = "starlark"
	EnginePlainWithCel string = "plain+cel"

	// DefaultEngine is used when a policy does not declare one
	DefaultEngine string = EngineCel
)

// Engine renders a template using the given context.
// Each engine lives in its own package and only knows about its own syntax.
type Engine interface {
	Name() string
	Evaluate(source string, data *InjectedData) (string, error)
}

// Registry keeps the available engines indexed by name
type Registry struct {
	engines map[string]Engine
}

func NewRegistry(engines ...Engine) *Registry {
	indexed := make(map[string]Engine, len(engines))
	for _, engine := range engines {
		indexed[engine.Name()] = engine
	}

	return &Registry{engines: indexed}
}

// Evaluate renders the template with the engine the policy asked for.
// An empty engine name falls back to DefaultEngine
func (r *Registry) Evaluate(engine, source string, data *InjectedData) (string, error) {

	if engine == "" {
		engine = DefaultEngine
	}

	selected, found := r.engines[engine]
	if !found {
		return "", fmt.Errorf("unknown template engine '%s'. Available: %s", engine, strings.Join(r.Names(), ", "))
	}

	return selected.Evaluate(source, data)
}

// Names returns the registered engine names, sorted
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.engines))
	for name := range r.engines {
		names = append(names, name)
	}

	slices.Sort(names)
	return names
}
