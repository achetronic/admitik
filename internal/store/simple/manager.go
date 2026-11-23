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
	"slices"

	//
	"golang.org/x/exp/maps"
)

func NewStore[T StoreResourceI]() *Store[T] {
	return &Store[T]{
		collections: make(map[string][]T),
	}
}

// AddOrUpdateResource adds an object to a collection.
// When an object with the same unique identifier already exists, it is replaced.
func (s *Store[T]) AddOrUpdateResource(collectionName string, object T) {

	s.mu.Lock()
	defer s.mu.Unlock()

	collection := s.collections[collectionName]
	for itemIndex, itemObject := range collection {
		if itemObject.GetUniqueIdentifier() == object.GetUniqueIdentifier() {
			collection[itemIndex] = object
			return
		}
	}

	s.collections[collectionName] = append(collection, object)
}

// RemoveResource deletes an object from a collection.
// The collection itself is removed when it becomes empty.
func (s *Store[T]) RemoveResource(collectionName string, object T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	collection := s.collections[collectionName]
	index := slices.IndexFunc(collection, func(item T) bool {
		return item.GetUniqueIdentifier() == object.GetUniqueIdentifier()
	})
	if index == -1 {
		return
	}

	s.collections[collectionName] = slices.Delete(collection, index, index+1)
	if len(s.collections[collectionName]) == 0 {
		delete(s.collections, collectionName)
	}
}

// GetResources returns a snapshot of the desired collection.
// The returned slice is a copy, so callers can iterate over it while other goroutines
// keep updating the store.
func (s *Store[T]) GetResources(collectionName string) []T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	collection, found := s.collections[collectionName]
	if !found {
		return []T{}
	}

	return slices.Clone(collection)
}

// GetCollectionNames returns a list of collection names.
// Collections are commonly named following the pattern: {group}/{version}/{resource}/{operation}
func (s *Store[T]) GetCollectionNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return maps.Keys(s.collections)
}

// SortCollection sorts a collection once in place. The sort is stable: objects considered
// equal by 'less' keep their insertion order.
func (s *Store[T]) SortCollection(
	collectionName string,
	less func(a, b T) bool,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	collection, found := s.collections[collectionName]
	if !found {
		return
	}

	slices.SortStableFunc(collection, func(a, b T) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		default:
			return 0
		}
	})
}
