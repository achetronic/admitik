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
	"slices"
	"strings"

	//
	starletconv "github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"

	//
	"github.com/freepik-company/admitik/internal/template"
)

// Engine evaluates Starlark scripts. Whatever the script prints is returned as the result
type Engine struct {
	programs *programCache
}

func New() *Engine {
	return &Engine{programs: newProgramCache()}
}

func (e *Engine) Name() string {
	return template.EngineStarlark
}

func (e *Engine) Evaluate(source string, data *template.InjectedData) (string, error) {

	values := data.ToMap()

	predeclared, err := predeclaredValues(values)
	if err != nil {
		return "", err
	}

	program, err := e.programs.get(source, contextKeys(values), sortedNames(predeclared))
	if err != nil {
		return "", err
	}

	prints := []string{}
	thread := &starlark.Thread{
		Name: "template",
		Print: func(_ *starlark.Thread, msg string) {
			prints = append(prints, msg)
		},
	}

	globals, err := program.Init(thread, predeclared)
	if err != nil {
		return "", fmt.Errorf("failed executing starlark code: %w", err)
	}

	// Values the script wrote into 'vars' are handed back, so the next template
	// of the same policy can read them
	if variables, found := globals["vars"]; found {
		converted, err := starletconv.Unmarshal(variables)
		if err != nil {
			return "", fmt.Errorf("failed converting Starlark 'vars' into Golang types: %w", err)
		}

		if variablesMap, isMap := converted.(map[string]any); isMap {
			data.Vars = variablesMap
		}
	}

	return strings.Join(prints, ""), nil
}

// contextKeys returns the names of the context values, sorted
func contextKeys(values map[string]any) []string {

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	slices.Sort(keys)
	return keys
}
