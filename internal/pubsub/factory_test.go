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

package pubsub

import (
	"sync"
	"testing"
	"time"
)

func TestPublishDeliversToEverySubscriber(t *testing.T) {
	broadcaster := NewBroadcaster[string]()

	first := broadcaster.Subscribe(1)
	second := broadcaster.Subscribe(1)

	broadcaster.Publish(Event[string]{Type: "add", Object: "policy"})

	for index, subscription := range []chan Event[string]{first, second} {
		select {
		case event := <-subscription:
			if event.Type != "add" || event.Object != "policy" {
				t.Errorf("subscriber %d got %+v, expected {add policy}", index, event)
			}
		default:
			t.Errorf("subscriber %d got no event", index)
		}
	}
}

func TestPublishDoesNotBlockWhenBufferIsFull(t *testing.T) {
	broadcaster := NewBroadcaster[string]()
	subscription := broadcaster.Subscribe(1)

	done := make(chan struct{})
	go func() {
		broadcaster.Publish(Event[string]{Type: "add", Object: "first"})
		broadcaster.Publish(Event[string]{Type: "add", Object: "second"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}

	// The channel keeps the first event and discards the ones that do not fit
	if event := <-subscription; event.Object != "first" {
		t.Errorf("got %+v, expected the first event", event)
	}
}

func TestUnsubscribeClosesAndStopsDelivery(t *testing.T) {
	broadcaster := NewBroadcaster[string]()
	subscription := broadcaster.Subscribe(1)

	broadcaster.Unsubscribe(subscription)

	if _, isOpen := <-subscription; isOpen {
		t.Error("subscription channel is still open after unsubscribe")
	}

	// Publishing with no subscribers must not panic
	broadcaster.Publish(Event[string]{Type: "add", Object: "policy"})
}

func TestUnsubscribeTwice(t *testing.T) {
	broadcaster := NewBroadcaster[string]()
	subscription := broadcaster.Subscribe(1)

	broadcaster.Unsubscribe(subscription)
	broadcaster.Unsubscribe(subscription)
}

func TestPublishWhileSubscribersComeAndGo(t *testing.T) {
	broadcaster := NewBroadcaster[int]()

	var waitGroup sync.WaitGroup
	stop := make(chan struct{})

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		for i := 0; i < 2000; i++ {
			broadcaster.Publish(Event[int]{Type: "add", Object: i})
		}
		close(stop)
	}()

	for i := 0; i < 4; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for {
				select {
				case <-stop:
					return
				default:
					subscription := broadcaster.Subscribe(1)
					broadcaster.Unsubscribe(subscription)
				}
			}
		}()
	}

	waitGroup.Wait()
}
