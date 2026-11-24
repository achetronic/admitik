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
	"sync"

	//
	"go.starlark.net/starlark"
)

// maxCachedObjects is the number of context objects held in the cache. A tree of a big corpus
// costs tens of megabytes, so the cache has to be bounded: rebuilding is cheaper than keeping
// every version of every policy forever
const maxCachedObjects = 50000

// treeCache keeps the values already converted for a given sources version, so a request does
// not pay the conversion again. Trees are frozen, so they can be shared between evaluations
type treeCache struct {
	mu sync.Mutex

	entries map[string]*treeEntry
	// order holds the keys from the most recently used to the least
	order []string

	objects    int
	maxObjects int
}

type treeEntry struct {
	value   starlark.Value
	objects int
}

func newTreeCache() *treeCache {
	return &treeCache{entries: map[string]*treeEntry{}, maxObjects: maxCachedObjects}
}

// get returns the cached value for the version, building it when missing.
// A version is not cached when it is empty or when its tree does not fit the budget
func (c *treeCache) get(version string, objects int, build func() (starlark.Value, error)) (starlark.Value, error) {

	if version == "" || objects > c.maxObjects {
		return build()
	}

	c.mu.Lock()
	entry, found := c.entries[version]
	if found {
		c.touch(version)
	}
	c.mu.Unlock()

	if found {
		return entry.value, nil
	}

	value, err := build()
	if err != nil {
		return nil, err
	}

	// The tree is shared from now on, so it must not change
	value.Freeze()

	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictUntilFits(objects)
	c.entries[version] = &treeEntry{value: value, objects: objects}
	c.order = append([]string{version}, c.order...)
	c.objects += objects

	return value, nil
}

// touch moves a key to the front of the usage order
func (c *treeCache) touch(version string) {

	for index, key := range c.order {
		if key != version {
			continue
		}

		c.order = append(c.order[:index], c.order[index+1:]...)
		c.order = append([]string{version}, c.order...)
		return
	}
}

// evictUntilFits drops the least recently used trees until the new one fits
func (c *treeCache) evictUntilFits(objects int) {

	for c.objects+objects > c.maxObjects && len(c.order) > 0 {
		oldest := c.order[len(c.order)-1]

		c.objects -= c.entries[oldest].objects
		delete(c.entries, oldest)
		c.order = c.order[:len(c.order)-1]
	}
}
