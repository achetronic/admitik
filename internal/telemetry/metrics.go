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
	"time"

	//
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Kind is what a policy does with a resource
type Kind string

const (
	KindMutate   Kind = "mutate"
	KindValidate Kind = "validate"
)

// Outcome is what the policy decided
type Outcome string

const (
	OutcomePatched    Outcome = "patched"
	OutcomeSkipped    Outcome = "skipped"
	OutcomeAllowed    Outcome = "allowed"
	OutcomeDenied     Outcome = "denied"
	OutcomePermissive Outcome = "permissive"
	OutcomeFailed     Outcome = "failed"
)

// Reason is why a policy could not be applied. It is a closed set: the error itself only goes to
// the logs, because a label with free text would grow without control
type Reason string

const (
	ReasonNone       Reason = "none"
	ReasonSources    Reason = "sources"
	ReasonConditions Reason = "conditions"
	ReasonTemplate   Reason = "template"
	ReasonPatch      Reason = "patch"
)

// Decision is one policy deciding over one object
type Decision struct {
	Kind      Kind
	Policy    string
	Operation string
	Resource  string
	Outcome   Outcome
	Reason    Reason
}

var (
	decisions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "admitik_decisions_total",
		Help: "Number of decisions taken by each policy.",
	}, []string{"kind", "policy", "operation", "resource", "outcome", "reason"})

	evaluation = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "admitik_evaluation_seconds",
		Help:    "Time spent evaluating one object with one policy.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	}, []string{"kind", "policy"})

	injected = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "admitik_injected_objects",
		Help:    "Source objects injected into the templates of one evaluation.",
		Buckets: []float64{1, 10, 100, 1000, 5000, 10000, 50000, 200000},
	}, []string{"kind", "policy"})
)

// Recorder publishes what the policies do. A nil recorder discards everything, which is what
// the tests and the code paths without metrics use
type Recorder struct {
	enabled bool
}

func NewRecorder() *Recorder {

	metrics.Registry.MustRegister(decisions, evaluation, injected)

	return &Recorder{enabled: true}
}

// Decision records what a policy decided over an object
func (r *Recorder) Decision(decision Decision) {

	if r == nil || !r.enabled {
		return
	}

	reason := decision.Reason
	if reason == "" {
		reason = ReasonNone
	}

	decisions.WithLabelValues(
		string(decision.Kind),
		decision.Policy,
		decision.Operation,
		decision.Resource,
		string(decision.Outcome),
		string(reason),
	).Inc()
}

// Evaluation records how long one policy took and how much it injected
func (r *Recorder) Evaluation(kind Kind, policy string, duration time.Duration, objects int) {

	if r == nil || !r.enabled {
		return
	}

	evaluation.WithLabelValues(string(kind), policy).Observe(duration.Seconds())
	injected.WithLabelValues(string(kind), policy).Observe(float64(objects))
}
