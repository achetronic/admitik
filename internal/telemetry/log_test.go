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
	"errors"
	"fmt"
	"strings"
	"testing"

	//
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"

	//
	"github.com/admitikcore/admitik/internal/common"
)

// capture keeps the lines written by a logger, so the tests can read them back
func capture(t *testing.T) (logr.Logger, *[]string) {
	t.Helper()

	lines := []string{}

	logger := funcr.New(func(prefix, args string) {
		lines = append(lines, prefix+args)
	}, funcr.Options{Verbosity: 1})

	return logger, &lines
}

func deploymentRef() common.ObjectRef {
	return common.ObjectRef{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team-a", Name: "checkout"}
}

func mutationLine() DecisionLine {
	return DecisionLine{
		Policy:    "inject-team-labels",
		Kind:      KindMutate,
		Object:    deploymentRef(),
		Operation: "CREATE",
		Result:    OutcomePatched,
		Reason:    ReasonNone,
	}
}

func TestLogDecisionWritesTheSameFieldsForEveryKind(t *testing.T) {
	logger, lines := capture(t)

	LogDecision(logger, mutationLine())

	if len(*lines) != 1 {
		t.Fatalf("got %d lines, expected 1", len(*lines))
	}

	line := (*lines)[0]
	for _, expected := range []string{
		`"policy"="inject-team-labels"`,
		`"policyKind"="mutate"`,
		`"operation"="CREATE"`,
		`"object"={"apiVersion"="apps/v1" "kind"="Deployment" "namespace"="team-a" "name"="checkout"}`,
		`"result"="patched"`,
		`"reason"="none"`,
	} {
		if !strings.Contains(line, expected) {
			t.Errorf("the line does not carry %s: %s", expected, line)
		}
	}
}

func TestLogDecisionNamesThePolicyThatFailed(t *testing.T) {
	logger, lines := capture(t)

	line := mutationLine()
	line.Result = OutcomeFailed
	line.Reason = ReasonTemplate
	line.Err = errors.New("failed executing starlark code: undefined: spec")

	LogDecision(logger, line)

	if len(*lines) != 1 {
		t.Fatalf("got %d lines, expected 1", len(*lines))
	}
	// funcr has no level prefixes: it writes the error as a field
	if !strings.Contains((*lines)[0], `"error"=`) {
		t.Errorf("got %q, expected an error line", (*lines)[0])
	}
	if !strings.Contains((*lines)[0], `"policy"="inject-team-labels"`) {
		t.Errorf("the line does not name the policy: %s", (*lines)[0])
	}
	if !strings.Contains((*lines)[0], `"reason"="template"`) {
		t.Errorf("the line does not say why: %s", (*lines)[0])
	}
}

func TestLogDecisionKeepsTheQuietResultsForTheDebug(t *testing.T) {
	cases := map[string]Outcome{
		"a policy that allowed": OutcomeAllowed,
		"a policy that skipped": OutcomeSkipped,
	}

	for name, result := range cases {
		t.Run(name, func(t *testing.T) {
			logger, lines := capture(t)

			line := mutationLine()
			line.Result = result

			LogDecision(logger, line)

			if len(*lines) != 1 {
				t.Fatalf("got %d lines, expected 1", len(*lines))
			}
			// funcr writes the verbosity as the level: 1 is what V(1) produces
			if !strings.Contains((*lines)[0], `"level"=1`) {
				t.Errorf("got %q, expected a debug line", (*lines)[0])
			}
		})
	}
}

func TestLogDecisionWritesTheMessageOnlyWhenThereIsOne(t *testing.T) {
	logger, lines := capture(t)

	line := mutationLine()
	line.Result = OutcomeDenied
	line.Reason = ReasonConditions

	LogDecision(logger, line)
	if strings.Contains((*lines)[0], `"message"`) {
		t.Errorf("got %q, expected no message", (*lines)[0])
	}

	line.Message = "the container 'api' must declare a cpu limit"
	LogDecision(logger, line)
	if !strings.Contains((*lines)[1], fmt.Sprintf(`"message"=%q`, line.Message)) {
		t.Errorf("got %q, expected the message", (*lines)[1])
	}
}

func TestLogDecisionAppendsTheExtraFields(t *testing.T) {
	logger, lines := capture(t)

	LogDecision(logger, mutationLine(), "duration", "312µs")

	if !strings.Contains((*lines)[0], `"duration"="312µs"`) {
		t.Errorf("got %q, expected the extra field", (*lines)[0])
	}
}
