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

package plain

import (
	"github.com/freepik-company/admitik/internal/template"
)

// Engine returns the source untouched. It is used for templates that do not need evaluation
type Engine struct{}

func New() *Engine {
	return &Engine{}
}

func (e *Engine) Name() string {
	return template.EnginePlain
}

func (e *Engine) Evaluate(source string, data *template.InjectedData) (string, error) {
	return source, nil
}
