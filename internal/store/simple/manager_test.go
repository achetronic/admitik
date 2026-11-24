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

package simple

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

type testObject struct {
	id       string
	group    string
	order    int
	revision int
}

func (o *testObject) GetUniqueIdentifier() string {
	return o.id
}

func ids(objects []*testObject) []string {
	results := make([]string, 0, len(objects))
	for _, item := range objects {
		results = append(results, item.id)
	}

	return results
}

func TestAddOrUpdateResource(t *testing.T) {
	tests := []struct {
		name       string
		operations []*testObject
		expected   []string
	}{
		{
			name:       "stores a new object",
			operations: []*testObject{{id: "a"}},
			expected:   []string{"a"},
		},
		{
			name:       "adds different objects to the same collection",
			operations: []*testObject{{id: "a"}, {id: "b"}},
			expected:   []string{"a", "b"},
		},
		{
			name:       "replaces an existing object instead of duplicating it",
			operations: []*testObject{{id: "a"}, {id: "b"}, {id: "a", revision: 2}},
			expected:   []string{"a", "b"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewStore[*testObject]()

			for _, object := range test.operations {
				store.AddOrUpdateResource("collection", object)
			}

			got := ids(store.GetResources("collection"))
			if !slices.Equal(got, test.expected) {
				t.Errorf("got %v, expected %v", got, test.expected)
			}
		})
	}
}

func TestAddOrUpdateResourceKeepsLatestRevision(t *testing.T) {
	store := NewStore[*testObject]()

	store.AddOrUpdateResource("collection", &testObject{id: "a", revision: 1})
	store.AddOrUpdateResource("collection", &testObject{id: "a", revision: 2})

	got := store.GetResources("collection")
	if len(got) != 1 {
		t.Fatalf("got %d objects, expected 1", len(got))
	}
	if got[0].revision != 2 {
		t.Errorf("got revision %d, expected 2", got[0].revision)
	}
}

func TestRemoveResource(t *testing.T) {
	tests := []struct {
		name              string
		stored            []string
		remove            string
		expectedIDs       []string
		collectionRemains bool
	}{
		{
			name:              "removes a single object",
			stored:            []string{"a", "b"},
			remove:            "a",
			expectedIDs:       []string{"b"},
			collectionRemains: true,
		},
		{
			name:              "removes the collection when it becomes empty",
			stored:            []string{"a"},
			remove:            "a",
			expectedIDs:       []string{},
			collectionRemains: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewStore[*testObject]()
			for _, id := range test.stored {
				store.AddOrUpdateResource("collection", &testObject{id: id})
			}

			store.RemoveResource("collection", &testObject{id: test.remove})

			got := ids(store.GetResources("collection"))
			if !slices.Equal(got, test.expectedIDs) {
				t.Errorf("got %v, expected %v", got, test.expectedIDs)
			}

			collectionExists := slices.Contains(store.GetCollectionNames(), "collection")
			if collectionExists != test.collectionRemains {
				t.Errorf("collection present = %t, expected %t", collectionExists, test.collectionRemains)
			}
		})
	}
}

func TestRemoveResourceIgnoresUnknownObjects(t *testing.T) {
	store := NewStore[*testObject]()
	store.AddOrUpdateResource("collection", &testObject{id: "a"})

	store.RemoveResource("collection", &testObject{id: "missing"})

	if got := ids(store.GetResources("collection")); !slices.Equal(got, []string{"a"}) {
		t.Errorf("got %v, expected [a]", got)
	}
}

func TestGetResourcesReturnsSnapshot(t *testing.T) {
	store := NewStore[*testObject]()
	store.AddOrUpdateResource("collection", &testObject{id: "a"})

	snapshot := store.GetResources("collection")
	snapshot[0] = &testObject{id: "mutated"}

	if got := ids(store.GetResources("collection")); !slices.Equal(got, []string{"a"}) {
		t.Errorf("the store was mutated through the returned slice: got %v, expected [a]", got)
	}
}

func TestGetResourcesOfUnknownCollection(t *testing.T) {
	store := NewStore[*testObject]()

	if got := store.GetResources("missing"); len(got) != 0 {
		t.Errorf("got %v, expected an empty list", got)
	}
}

func TestSortCollection(t *testing.T) {
	// Objects sharing the same 'order' must keep their insertion order, so the
	// result must match a stable sort whatever the size of the collection is
	const groups = 8

	store := NewStore[*testObject]()
	for i := 0; i < groups; i++ {
		store.AddOrUpdateResource("collection", &testObject{id: fmt.Sprintf("high-%d", i), group: "high", order: 1})
		store.AddOrUpdateResource("collection", &testObject{id: fmt.Sprintf("low-%d", i), group: "low", order: 0})
		store.AddOrUpdateResource("collection", &testObject{id: fmt.Sprintf("mid-%d", i), group: "mid", order: 2})
	}

	store.SortCollection("collection", func(a, b *testObject) bool {
		return a.order < b.order
	})

	got := store.GetResources("collection")

	expected := []string{}
	for _, group := range []string{"low", "high", "mid"} {
		for i := 0; i < groups; i++ {
			expected = append(expected, group)
		}
	}

	gotGroups := make([]string, 0, len(got))
	for _, object := range got {
		gotGroups = append(gotGroups, object.group)
	}

	if !slices.Equal(gotGroups, expected) {
		t.Errorf("got %v, expected %v", gotGroups, expected)
	}
}

func TestSortCollectionOfUnknownCollection(t *testing.T) {
	store := NewStore[*testObject]()

	store.SortCollection("missing", func(a, b *testObject) bool {
		return a.order < b.order
	})
}

func TestConcurrentAccess(t *testing.T) {
	store := NewStore[*testObject]()
	for i := 0; i < 5; i++ {
		store.AddOrUpdateResource("collection", &testObject{id: string(rune('a' + i)), order: i})
	}

	var waitGroup sync.WaitGroup
	stop := make(chan struct{})

	// Writer: sorts and updates while readers iterate, like the mutation policies controller does
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		for i := 0; i < 1000; i++ {
			store.SortCollection("collection", func(a, b *testObject) bool {
				return a.order < b.order
			})
			store.AddOrUpdateResource("collection", &testObject{id: "z", order: i, revision: i})
		}
		close(stop)
	}()

	// Readers: take snapshots while the writer mutates, like the admission server does
	for i := 0; i < 4; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for {
				select {
				case <-stop:
					return
				default:
					for _, object := range store.GetResources("collection") {
						_ = object.order
						_ = object.revision
					}
					_ = store.GetCollectionNames()
				}
			}
		}()
	}

	waitGroup.Wait()
}
