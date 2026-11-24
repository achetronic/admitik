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

package policystore

import (
	"github.com/admitikcore/admitik/internal/store/simple"
)

// PolicyStore holds the policies of a single type and exposes the mutations done over them
type PolicyStore[T PolicyResourceI] struct {
	// Embed a store to put all the policies
	// This will be used by third-party controllers to evaluate policies
	*simple.Store[T]
}
