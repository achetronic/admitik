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
	"slices"
	"sync"

	//
	modStarlarkJson "go.starlark.net/lib/json"
	modStarlarkMath "go.starlark.net/lib/math"
	modStarlarkTime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"

	//
	modStarletBase64 "github.com/1set/starlet/lib/base64"
	modStarletCsv "github.com/1set/starlet/lib/csv"
	modStarletHashlib "github.com/1set/starlet/lib/hashlib"
	modStarletHttp "github.com/1set/starlet/lib/http"
	modStarletLog "github.com/1set/starlet/lib/log"
	modStarletNet "github.com/1set/starlet/lib/net"
	modStarletRandom "github.com/1set/starlet/lib/random"
	modStarletRe "github.com/1set/starlet/lib/re"
	modStarletString "github.com/1set/starlet/lib/string"

	//
	modSelfYaml "github.com/freepik-company/admitik/internal/template/starlarkmods/yaml"
)

// modules is loaded once. It does not depend on the evaluation context
var modules = sync.OnceValues(func() (starlark.StringDict, error) {

	starletBase64, err := modStarletBase64.LoadModule()
	if err != nil {
		return nil, fmt.Errorf("failed loading module base64: %w", err)
	}

	starletCsv, err := modStarletCsv.LoadModule()
	if err != nil {
		return nil, fmt.Errorf("failed loading module csv: %w", err)
	}

	starletHashlib, err := modStarletHashlib.LoadModule()
	if err != nil {
		return nil, fmt.Errorf("failed loading module hashlib: %w", err)
	}

	starletHttp, err := modStarletHttp.LoadModule()
	if err != nil {
		return nil, fmt.Errorf("failed loading module http: %w", err)
	}

	starletLog, err := modStarletLog.LoadModule()
	if err != nil {
		return nil, fmt.Errorf("failed loading module log: %w", err)
	}

	starletNet, err := modStarletNet.LoadModule()
	if err != nil {
		return nil, fmt.Errorf("failed loading module net: %w", err)
	}

	starletRandom, err := modStarletRandom.LoadModule()
	if err != nil {
		return nil, fmt.Errorf("failed loading module random: %w", err)
	}

	starletRe, err := modStarletRe.LoadModule()
	if err != nil {
		return nil, fmt.Errorf("failed loading module re: %w", err)
	}

	starletString, err := modStarletString.LoadModule()
	if err != nil {
		return nil, fmt.Errorf("failed loading module string: %w", err)
	}

	return starlark.StringDict{
		"math":    modStarlarkMath.Module,
		"json":    modStarlarkJson.Module,
		"time":    modStarlarkTime.Module,
		"base64":  starletBase64["base64"],
		"csv":     starletCsv["csv"],
		"hashlib": starletHashlib["hashlib"],
		"http":    starletHttp["http"],
		"log":     starletLog["log"],
		"net":     starletNet["net"],
		"random":  starletRandom["random"],
		"re":      starletRe["re"],
		"string":  starletString["string"],
		"yaml":    modSelfYaml.Module,
	}, nil
})

// sortedNames returns the predeclared names sorted, so they can be used as a cache key
func sortedNames(predeclared starlark.StringDict) []string {

	names := make([]string, 0, len(predeclared))
	for name := range predeclared {
		names = append(names, name)
	}

	slices.Sort(names)
	return names
}
