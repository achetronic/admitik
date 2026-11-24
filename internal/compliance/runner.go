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

package compliance

import (
	"context"
	"time"

	//
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	//
	"github.com/admitikcore/admitik/internal/telemetry"
)

// Runner scans the cluster every interval. It asks for leader election, so only the elected
// replica runs the scans
type Runner struct {
	// Interval between scans
	Interval time.Duration

	// Scanner evaluates the objects that already exist
	Scanner *Scanner

	// Metrics publishes what the last scan found
	Metrics *telemetry.Recorder
}

var _ manager.LeaderElectionRunnable = Runner{}

// NeedLeaderElection implements manager.LeaderElectionRunnable
func (r Runner) NeedLeaderElection() bool {
	return true
}

// Start scans once and then every interval until the manager stops
func (r Runner) Start(ctx context.Context) error {

	logger := log.FromContext(ctx).WithValues("task", "compliance")
	logger.Info("Starting the compliance scans", "interval", r.Interval.String())

	r.scan(ctx)

	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping the compliance scans")
			return nil
		case <-ticker.C:
			r.scan(ctx)
		}
	}
}

// scan evaluates the objects that already exist and publishes the outcome
func (r Runner) scan(ctx context.Context) {

	logger := log.FromContext(ctx).WithValues("task", "compliance")

	scan, err := r.Scanner.Scan(ctx)
	if err != nil {
		logger.Error(err, "Failed running the compliance scan")
		return
	}

	r.Metrics.Compliance(ToTelemetry(scan.Results))

	logger.Info("Compliance scan finished",
		"policies", scan.Summary.Policies,
		"scanned", scan.Summary.Scanned,
		"nonCompliant", scan.Summary.NonCompliant,
		"errors", scan.Summary.Errors,
		"duration", scan.Duration.String())
}
