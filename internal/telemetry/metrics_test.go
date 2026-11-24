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
	"testing"

	//
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestRecorder() *Recorder {
	return &Recorder{enabled: true}
}

func TestComplianceReplacesTheResultsOfTheLastScan(t *testing.T) {
	compliance.Reset()

	recorder := newTestRecorder()
	recorder.Compliance([]ComplianceResult{
		{Policy: "require-limits", Resource: "v1/pods", Compliant: 8, NonCompliant: 2, Errors: 1},
	})

	if got := testutil.CollectAndCount(compliance, "admitik_compliance_objects"); got != 3 {
		t.Fatalf("got %d series after the first scan, expected 3", got)
	}

	// A policy that goes away must not leave its series behind
	recorder.Compliance([]ComplianceResult{
		{Policy: "require-team-label", Resource: "v1/pods", Compliant: 10},
	})

	if got := testutil.CollectAndCount(compliance, "admitik_compliance_objects"); got != 3 {
		t.Fatalf("got %d series after the second scan, expected the 3 of the new policy", got)
	}

	value := testutil.ToFloat64(compliance.WithLabelValues("require-team-label", "v1/pods", ComplianceCompliant))
	if value != 10 {
		t.Errorf("got %v as the compliant objects, expected 10", value)
	}

	compliance.Reset()
}

func TestComplianceOfAnEmptyScanClearsEverything(t *testing.T) {
	compliance.Reset()

	recorder := newTestRecorder()
	recorder.Compliance([]ComplianceResult{{Policy: "require-limits", Resource: "v1/pods", Compliant: 1}})
	recorder.Compliance(nil)

	if got := testutil.CollectAndCount(compliance, "admitik_compliance_objects"); got != 0 {
		t.Errorf("got %d series, expected none", got)
	}
}

func TestPoliciesRecordsHowManyAreLoaded(t *testing.T) {
	recorder := newTestRecorder()
	recorder.Policies(KindValidate, 12)

	if value := testutil.ToFloat64(policies.WithLabelValues(string(KindValidate))); value != 12 {
		t.Errorf("got %v, expected 12", value)
	}
}

func TestDecisionCountsTheUnknownReasonAsNone(t *testing.T) {
	recorder := newTestRecorder()
	recorder.Decision(Decision{
		Kind:      KindValidate,
		Policy:    "require-limits",
		Operation: "CREATE",
		Resource:  "v1/pods",
		Outcome:   OutcomeAllowed,
	})

	value := testutil.ToFloat64(decisions.WithLabelValues(
		string(KindValidate), "require-limits", "CREATE", "v1/pods", string(OutcomeAllowed), string(ReasonNone)))

	if value != 1 {
		t.Errorf("got %v, expected the decision counted with the 'none' reason", value)
	}
}

func TestANilRecorderDiscardsEverything(t *testing.T) {
	var recorder *Recorder

	recorder.Decision(Decision{Kind: KindGenerate, Policy: "unused"})
	recorder.Evaluation(KindGenerate, "unused", 0, 0)
	recorder.Policies(KindGenerate, 1)
	recorder.Compliance([]ComplianceResult{{Policy: "unused"}})
}

func TestCollectorSumsEveryCounterItIsGiven(t *testing.T) {
	recorder := newTestRecorder()

	collector := Collector{
		Recorder: recorder,
		Counters: map[Kind]PolicyCounter{
			KindValidate: func() int { return 4 },
			KindMutate:   func() int { return 2 },
			KindGenerate: nil,
		},
	}

	collector.collect()

	if value := testutil.ToFloat64(policies.WithLabelValues(string(KindValidate))); value != 4 {
		t.Errorf("got %v validation policies, expected 4", value)
	}
	if value := testutil.ToFloat64(policies.WithLabelValues(string(KindMutate))); value != 2 {
		t.Errorf("got %v mutation policies, expected 2", value)
	}
}
