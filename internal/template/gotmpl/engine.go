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

package gotmpl

import (
	"bytes"
	"sync"
	textTemplate "text/template"

	//
	"golang.org/x/exp/maps"

	//
	"github.com/freepik-company/admitik/internal/template"
)

// Engine renders Go templates with the Helm-like function map
type Engine struct{}

func New() *Engine {
	return &Engine{}
}

func (e *Engine) Name() string {
	return template.EngineGotmpl
}

func (e *Engine) Evaluate(source string, data *template.InjectedData) (string, error) {

	parsed, err := textTemplate.New("main").Funcs(functionsFor(data)).Parse(source)
	if err != nil {
		return "", err
	}

	buffer := new(bytes.Buffer)
	if err := parsed.Execute(buffer, data.ToMap()); err != nil {
		return "", err
	}

	return buffer.String(), nil
}

// baseFunctions is built once. sprig builds a big map and it does not depend on the context
var baseFunctions = sync.OnceValue(func() textTemplate.FuncMap {
	return FunctionsMap()
})

// functionsFor returns the function map for one evaluation.
// 'setVar' is added per evaluation because it writes into the context of the current request
func functionsFor(data *template.InjectedData) textTemplate.FuncMap {

	functions := make(textTemplate.FuncMap, len(baseFunctions())+1)
	maps.Copy(functions, baseFunctions())

	functions["setVar"] = func(key string, value any) string {
		data.Vars[key] = value
		return ""
	}

	return functions
}
