package sources

import (
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TODO
type resourceTypeName string

// TODO
type resourceTypeWatcherT struct {
	// Enforce concurrency safety
	Mutex *sync.RWMutex

	// Started represents a flag to know if the watcher is running
	Started *bool

	// Blocked represents a flag to prevent watcher from starting
	Blocked *bool

	// StopSignal represents a flag to kill the watcher.
	// Watcher will be potentially re-launched by SourcesController
	StopSignal *chan bool

	//
	ResourceList []*unstructured.Unstructured
}

type WatcherPoolT struct {
	// Enforce concurrency safety
	Mutex *sync.RWMutex

	Pool map[resourceTypeName]*resourceTypeWatcherT
}
