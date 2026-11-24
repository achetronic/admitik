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
	"context"
	"time"

	//
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// PolicyCounter returns how many policies of one kind are loaded
type PolicyCounter func() int

// Collector publishes the number of policies of each kind, so a replica that loses them all is
// visible without waiting for a request
type Collector struct {
	// Interval between collections
	Interval time.Duration

	Recorder *Recorder

	// Counters holds how to count the policies of each kind
	Counters map[Kind]PolicyCounter
}

var _ manager.Runnable = Collector{}

// Start collects the policy counts until the manager stops
func (c Collector) Start(ctx context.Context) error {

	c.collect()

	if c.Interval <= 0 {
		return nil
	}

	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.collect()
		}
	}
}

func (c Collector) collect() {

	for kind, counter := range c.Counters {
		if counter == nil {
			continue
		}

		c.Recorder.Policies(kind, counter())
	}
}
