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

package audit

import (
	"context"
	"time"

	//
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Runner scans the cluster every interval. It asks for leader election, so only the elected
// replica runs the scans
type Runner struct {
	// Interval between scans
	Interval time.Duration

	// Scanner builds the report
	Scanner *Scanner
}

var _ manager.LeaderElectionRunnable = Runner{}

// NeedLeaderElection implements manager.LeaderElectionRunnable
func (r Runner) NeedLeaderElection() bool {
	return true
}

// Start runs a scan every interval until the manager stops
func (r Runner) Start(ctx context.Context) error {

	logger := log.FromContext(ctx).WithValues("task", "audit")
	logger.Info("Starting the audit scans", "interval", r.Interval.String())

	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping the audit scans")
			return nil
		case <-ticker.C:
			report, err := r.Scanner.Scan(ctx)
			if err != nil {
				logger.Error(err, "Failed running the audit scan")
				continue
			}

			logger.Info("Audit scan finished",
				"scanned", report.Status.Summary.Scanned,
				"nonCompliant", report.Status.Summary.NonCompliant,
				"duration", report.Status.Duration)
		}
	}
}
