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

package cel

import (
	"fmt"
	"sync"

	//
	"github.com/google/cel-go/cel"

	//
	"github.com/freepik-company/admitik/internal/template"
)

// Expression evaluates the whole source as a single CEL expression
type Expression struct {
	env *cel.Env
	mu  sync.RWMutex
	// programs are indexed by CEL source. The set of sources comes from the policies,
	// so it is bounded by the number of policies in the cluster
	programs map[string]cel.Program
}

func NewExpression() (*Expression, error) {

	env, err := cel.NewEnv(variables()...)
	if err != nil {
		return nil, fmt.Errorf("error creating CEL environment: %w", err)
	}

	return &Expression{env: env, programs: map[string]cel.Program{}}, nil
}

func (e *Expression) Name() string {
	return template.EngineCel
}

func (e *Expression) Evaluate(source string, data *template.InjectedData) (string, error) {

	program, err := e.program(source)
	if err != nil {
		return "", err
	}

	out, _, err := program.Eval(activation(data))
	if err != nil {
		return "", fmt.Errorf("program evaluation error: %w", err)
	}

	return fmt.Sprintf("%v", out.Value()), nil
}

// program returns the compiled program for the given source, compiling it only the first time
func (e *Expression) program(source string) (cel.Program, error) {

	e.mu.RLock()
	program, found := e.programs[source]
	e.mu.RUnlock()

	if found {
		return program, nil
	}

	ast, issues := e.env.Compile(source)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("type-check error: %w", issues.Err())
	}

	program, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program construction error: %w", err)
	}

	e.mu.Lock()
	e.programs[source] = program
	e.mu.Unlock()

	return program, nil
}

// variables declares every value exposed to CEL as dynamic.
// 'vars' is excluded on purpose: CEL expressions must not depend on values
// written by a previous template evaluation
func variables() []cel.EnvOption {

	options := []cel.EnvOption{}
	for key := range template.NewInjectedData().ToMap() {
		if key == "vars" {
			continue
		}

		options = append(options, cel.Variable(key, cel.DynType))
	}

	return options
}

func activation(data *template.InjectedData) map[string]any {
	values := data.ToMap()
	delete(values, "vars")

	return values
}
