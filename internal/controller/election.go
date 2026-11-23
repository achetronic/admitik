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

package controller

import (
	"context"

	//
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ElectionRefresh runs a task once this replica becomes the leader and then waits for the
// context to finish. Only the leader writes the webhook configurations, so a replica that takes
// over has to rewrite them: the replicas that were not leading never did it.
// It is added to the manager without asking to skip leader election, so Controller Runtime only
// starts it on the elected replica
type ElectionRefresh struct {
	// Name identifies the task in the logs
	Name string

	// Refresh is the task to run when this replica becomes the leader
	Refresh func(ctx context.Context) error
}

var _ manager.LeaderElectionRunnable = ElectionRefresh{}

// NeedLeaderElection implements manager.LeaderElectionRunnable
func (e ElectionRefresh) NeedLeaderElection() bool {
	return true
}

// Start runs the refresh and then keeps the runnable alive until the manager stops
func (e ElectionRefresh) Start(ctx context.Context) error {

	if err := e.Refresh(ctx); err != nil {
		log.FromContext(ctx).Error(err, "Failed refreshing resources after becoming the leader", "task", e.Name)
	}

	<-ctx.Done()
	return nil
}
