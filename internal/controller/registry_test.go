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

package controller

import (
	"testing"

	//
	"github.com/admitikcore/admitik/internal/store/simple"
)

type registryItem struct {
	name string
}

func (i *registryItem) GetUniqueIdentifier() string { return i.name }

func TestRemoveFromRegistryDropsEveryCollection(t *testing.T) {
	store := simple.NewStore[*registryItem]()

	// The same object indexed under several collections, as a policy is
	store.AddOrUpdateResource("a", &registryItem{name: "ghost"})
	store.AddOrUpdateResource("b", &registryItem{name: "ghost"})
	store.AddOrUpdateResource("a", &registryItem{name: "keeper"})

	RemoveFromRegistry(store, "ghost")

	for _, collection := range store.GetCollectionNames() {
		for _, item := range store.GetResources(collection) {
			if item.GetUniqueIdentifier() == "ghost" {
				t.Errorf("the ghost is still in the collection %q", collection)
			}
		}
	}

	if got := len(store.GetResources("a")); got != 1 {
		t.Errorf("got %d entries in 'a', expected the keeper to stay", got)
	}
}

func TestRemoveFromRegistryOfSomethingThatIsNotThere(t *testing.T) {
	store := simple.NewStore[*registryItem]()
	store.AddOrUpdateResource("a", &registryItem{name: "keeper"})

	RemoveFromRegistry(store, "ghost")

	if got := len(store.GetResources("a")); got != 1 {
		t.Errorf("got %d entries, expected 1", got)
	}
}
