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
	"regexp"
	"strings"

	//
	"github.com/freepik-company/admitik/internal/template"
)

var placeholderRegex = regexp.MustCompile(`{{cel:\s*([\s\S]*?)\s*}}`)

// Placeholders replaces every {{cel: ...}} occurrence in a plain text with its result
type Placeholders struct {
	expression *Expression
}

func NewPlaceholders(expression *Expression) *Placeholders {
	return &Placeholders{expression: expression}
}

func (p *Placeholders) Name() string {
	return template.EnginePlainWithCel
}

func (p *Placeholders) Evaluate(source string, data *template.InjectedData) (string, error) {

	result := source
	for _, match := range placeholderRegex.FindAllStringSubmatch(source, -1) {
		if len(match) < 2 {
			continue
		}

		evaluated, err := p.expression.Evaluate(match[1], data)
		if err != nil {
			return "", fmt.Errorf("error evaluating CEL expression '%s': %w", match[1], err)
		}

		result = strings.Replace(result, match[0], evaluated, 1)
	}

	return result, nil
}
