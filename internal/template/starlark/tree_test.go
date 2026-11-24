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
	"testing"

	//
	"go.starlark.net/starlark"
)

func TestSourcesTreeIsReusedByVersion(t *testing.T) {
	engine := New()
	values := configMaps(3).ToMap()

	first, err := engine.predeclared(values, "version-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := engine.predeclared(values, "version-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first["__raw_sources"] != second["__raw_sources"] {
		t.Error("the sources tree was built twice for the same version")
	}
}

func TestSourcesTreeIsRebuiltWithAnotherVersion(t *testing.T) {
	engine := New()
	values := configMaps(3).ToMap()

	first, err := engine.predeclared(values, "version-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := engine.predeclared(values, "version-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first["__raw_sources"] == second["__raw_sources"] {
		t.Error("the sources tree was reused with another version")
	}
}

func TestSourcesTreeIsNotCachedWithoutVersion(t *testing.T) {
	engine := New()
	values := configMaps(3).ToMap()

	first, err := engine.predeclared(values, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := engine.predeclared(values, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first["__raw_sources"] == second["__raw_sources"] {
		t.Error("the sources tree was cached without a version")
	}
}

func TestSourcesTreeIsFrozen(t *testing.T) {
	engine := New()
	values := configMaps(1).ToMap()

	predeclared, err := engine.predeclared(values, "version-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sources, isDict := predeclared["__raw_sources"].(*starlark.Dict)
	if !isDict {
		t.Fatalf("sources is not a dict: %T", predeclared["__raw_sources"])
	}

	// A frozen tree cannot be changed, which is what makes it safe to share
	if err := sources.SetKey(starlark.MakeInt(0), starlark.None); err == nil {
		t.Error("the cached sources tree is not frozen")
	}
}

func TestTreeCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	cache := newTreeCache()
	cache.maxObjects = 10

	build := func() (starlark.Value, error) {
		return starlark.NewDict(1), nil
	}

	for _, version := range []string{"a", "b", "c"} {
		if _, err := cache.get(version, 4, build); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(cache.entries) != 2 {
		t.Fatalf("got %d cached trees, expected 2", len(cache.entries))
	}
	if _, found := cache.entries["a"]; found {
		t.Error("the oldest tree was not evicted")
	}
}

func TestTreeCacheSkipsTreesLargerThanItsBudget(t *testing.T) {
	cache := newTreeCache()
	cache.maxObjects = 10

	if _, err := cache.get("big", 11, func() (starlark.Value, error) {
		return starlark.NewDict(1), nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cache.entries) != 0 {
		t.Errorf("got %d cached trees, expected none", len(cache.entries))
	}
}

func TestTreeCacheKeepsTheMostRecentlyUsed(t *testing.T) {
	cache := newTreeCache()
	cache.maxObjects = 10

	build := func() (starlark.Value, error) {
		return starlark.NewDict(1), nil
	}

	for _, version := range []string{"a", "b"} {
		if _, err := cache.get(version, 4, build); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Using 'a' makes 'b' the oldest one
	if _, err := cache.get("a", 4, build); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := cache.get("c", 4, build); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, found := cache.entries["b"]; found {
		t.Error("'b' should have been evicted before 'a'")
	}
	if _, found := cache.entries["a"]; !found {
		t.Error("'a' was evicted although it was the most recently used")
	}
}
