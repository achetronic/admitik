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
	"strings"
	"sync"

	//
	"go.starlark.net/starlark"
	starlarksyntax "go.starlark.net/syntax"
)

// programCache keeps the compiled scripts indexed by source and by the names exposed to them.
// The sources come from the policies, so the cache is bounded by the number of policies
type programCache struct {
	mu       sync.RWMutex
	programs map[string]*starlark.Program
}

func newProgramCache() *programCache {
	return &programCache{programs: map[string]*starlark.Program{}}
}

func (c *programCache) get(source string, contextKeys, predeclaredNames []string) (*starlark.Program, error) {

	key := strings.Join(contextKeys, "\x00") + "\x01" + strings.Join(predeclaredNames, "\x00") + "\x01" + source

	c.mu.RLock()
	program, found := c.programs[key]
	c.mu.RUnlock()

	if found {
		return program, nil
	}

	_, program, err := starlark.SourceProgramOptions(
		&starlarksyntax.FileOptions{},
		"template.star",
		declarations(contextKeys)+source,
		nameSet(predeclaredNames),
	)
	if err != nil {
		return nil, fmt.Errorf("failed compiling starlark code: %w", err)
	}

	c.mu.Lock()
	c.programs[key] = program
	c.mu.Unlock()

	return program, nil
}

// declarations binds every value of the context to a top level name,
// so templates can use 'object' instead of the internal prefixed name
func declarations(contextKeys []string) string {

	lines := make([]string, 0, len(contextKeys))
	for _, key := range contextKeys {
		lines = append(lines, fmt.Sprintf("%s = __raw_%s", key, key))
	}

	return strings.Join(lines, "\n") + "\n\n"
}

func nameSet(names []string) func(string) bool {
	indexed := make(map[string]struct{}, len(names))
	for _, name := range names {
		indexed[name] = struct{}{}
	}

	return func(name string) bool {
		_, found := indexed[name]
		return found
	}
}
