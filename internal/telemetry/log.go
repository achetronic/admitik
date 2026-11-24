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

package telemetry

import (
	//
	"github.com/go-logr/logr"

	//
	"github.com/admitikcore/admitik/internal/common"
)

// DecisionLine is one policy over one object: which policy, what kind, over which object, in
// which operation, and what came out of it
type DecisionLine struct {
	Policy    string
	Kind      Kind
	Object    common.ObjectRef
	Operation string

	// Result is what the policy did. It is called 'outcome' in the code and 'result' everywhere
	// the user reads it
	Result Outcome

	Reason  Reason
	Message string

	// Err is what went wrong. When it is set, the line is written at the error level
	Err error
}

// LogDecision writes one line about one policy over one object. Every kind of policy writes the
// same fields, so a query over the logs works across all of them
func LogDecision(logger logr.Logger, line DecisionLine, extra ...any) {

	fields := []any{
		"policy", line.Policy,
		"policyKind", line.Kind,
		"operation", line.Operation,
		"object", line.Object,
		"result", line.Result,
		"reason", line.Reason,
	}

	if line.Message != "" {
		fields = append(fields, "message", line.Message)
	}

	fields = append(fields, extra...)

	switch {
	case line.Err != nil:
		logger.Error(line.Err, "policy decision", fields...)
	case changedSomething(line.Result):
		logger.Info("policy decision", fields...)
	default:
		// A policy that did nothing only matters when somebody is debugging
		logger.V(1).Info("policy decision", fields...)
	}
}

// changedSomething tells whether a result is worth a line at info
func changedSomething(result Outcome) bool {

	switch result {
	case OutcomeDenied, OutcomePermissive, OutcomePatched, OutcomeGenerated, OutcomeFailed:
		return true
	}

	return false
}
