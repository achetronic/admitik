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

	//
	"go.starlark.net/starlark"
)

// toStarlark converts a value of the evaluation context into its Starlark representation.
// The context always comes from JSON (an AdmissionReview or an unstructured object), so the
// accepted types are a closed set: map, slice, string, number, bool and nil.
func toStarlark(value any) (starlark.Value, error) {

	switch typed := value.(type) {

	case nil:
		return starlark.None, nil

	case bool:
		return starlark.Bool(typed), nil

	case string:
		return starlark.String(typed), nil

	// Numbers are floats, as they are when they arrive from JSON
	case float64:
		return starlark.Float(typed), nil
	case float32:
		return starlark.Float(typed), nil
	case int:
		return starlark.Float(typed), nil
	case int64:
		return starlark.Float(typed), nil

	case []any:
		items := make([]starlark.Value, len(typed))
		for index, item := range typed {
			converted, err := toStarlark(item)
			if err != nil {
				return nil, err
			}
			items[index] = converted
		}

		return starlark.NewList(items), nil

	// The items of a source are always objects
	case []map[string]any:
		items := make([]starlark.Value, len(typed))
		for index, item := range typed {
			converted, err := toStarlark(item)
			if err != nil {
				return nil, err
			}
			items[index] = converted
		}

		return starlark.NewList(items), nil

	case []string:
		items := make([]starlark.Value, len(typed))
		for index, item := range typed {
			items[index] = starlark.String(item)
		}

		return starlark.NewList(items), nil

	case map[string]any:
		return toStarlarkDict(typed)

	// The policy sources are indexed by their position in the policy spec, so the keys are
	// integers, the same index CEL uses
	case map[int][]map[string]any:
		dict := starlark.NewDict(len(typed))
		for key, items := range typed {
			converted, err := toStarlark(items)
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.MakeInt(key), converted); err != nil {
				return nil, err
			}
		}

		return dict, nil

	default:
		return nil, fmt.Errorf("unsupported type %T in the evaluation context", value)
	}
}

func toStarlarkDict(source map[string]any) (*starlark.Dict, error) {

	dict := starlark.NewDict(len(source))
	for key, item := range source {
		converted, err := toStarlark(item)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		if err := dict.SetKey(starlark.String(key), converted); err != nil {
			return nil, err
		}
	}

	return dict, nil
}
