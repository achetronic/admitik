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

package informer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	//
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultResyncPeriod = 10 * time.Minute

	//
	informerStartedMessage         = "Informer has been started"
	informerStoppedMessage         = "Informer has been stopped"
	informerContextFinishedMessage = "Informer finished by context"
	informerKilledMessage          = "Informer killed by StopSignal"
)

// EventHandlerFunc is the signature of the functions called for every event received by an informer
type EventHandlerFunc = func(eventType watch.EventType, obj *unstructured.Unstructured, oldObj *unstructured.Unstructured) error

// Options configures an informer
type Options struct {
	// GVR of the watched resource
	GVR schema.GroupVersionResource

	// Namespace to watch. Empty means all namespaces
	Namespace string

	// InformerDurationToResync is the interval used by the informer to resync its cache.
	// Defaults to 10 minutes when zero
	InformerDurationToResync time.Duration

	// Optional: additional filters
	LabelSelector string
	FieldSelector string
}

// Dependencies are the external collaborators an informer needs
type Dependencies struct {
	Context *context.Context
	Client  dynamic.Interface
}

// Informer wraps a client-go shared informer, caching the objects of a single GVR
type Informer struct {
	mu sync.RWMutex

	//
	options      Options
	dependencies Dependencies

	//
	rawInformer cache.SharedIndexInformer
	stopChan    chan struct{}
	stopOnce    sync.Once

	//
	started bool
}

func NewInformer(opts Options, deps Dependencies) (*Informer, error) {

	if deps.Client == nil {
		return nil, errors.New("kube client cannot be nil")
	}

	if deps.Context == nil {
		return nil, errors.New("context cannot be nil")
	}

	if opts.InformerDurationToResync == 0 {
		opts.InformerDurationToResync = defaultResyncPeriod
	}

	tmpInformer := &Informer{
		options:      opts,
		dependencies: deps,

		stopChan: make(chan struct{}),
		started:  false,
	}

	//
	tmpInformer.createInformer()
	return tmpInformer, nil
}

func (r *Informer) createInformer() {
	// Include the namespace when defined by the user (used as filter)
	namespace := corev1.NamespaceAll
	if r.options.Namespace != "" {
		namespace = r.options.Namespace
	}

	// Include the name when defined by the user (used as filter)
	var tweakListOptions dynamicinformer.TweakListOptionsFunc
	if r.options.LabelSelector != "" || r.options.FieldSelector != "" {
		tweakListOptions = func(options *metav1.ListOptions) {
			if r.options.LabelSelector != "" {
				options.LabelSelector = r.options.LabelSelector
			}
			if r.options.FieldSelector != "" {
				options.FieldSelector = r.options.FieldSelector
			}
		}
	}

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		r.dependencies.Client,
		r.options.InformerDurationToResync,
		namespace,
		tweakListOptions)

	r.rawInformer = factory.ForResource(r.options.GVR).Informer()
}

// WithEventHandlerFunc registers the function called for every add/update/delete event.
// It must be used before calling Start
func (r *Informer) WithEventHandlerFunc(evHandlerFunc EventHandlerFunc) error {

	r.mu.RLock()
	started := r.started
	r.mu.RUnlock()

	if started {
		return errors.New("cannot add event handler after informer has started")
	}

	baseLogger := log.FromContext(*r.dependencies.Context).
		WithValues("gvr", r.options.GVR, "namespace", r.options.Namespace)

	// Register functions to handle different types of events
	handlers := cache.ResourceEventHandlerFuncs{

		//
		AddFunc: func(eventObject interface{}) {
			obj, isUnstructured := eventObject.(*unstructured.Unstructured)
			if !isUnstructured {
				return
			}

			logger := baseLogger.WithValues("eventType", watch.Added, "object", obj.GetName())
			logger.V(1).Info("Processing reconciliation")

			if err := evHandlerFunc(watch.Added, obj, nil); err != nil {
				logger.Error(err, "Error in add reconciliation")
			}
		},

		//
		UpdateFunc: func(eventObjectOld, eventObject interface{}) {
			obj, isUnstructured := eventObject.(*unstructured.Unstructured)
			if !isUnstructured {
				return
			}

			oldObj, isUnstructured := eventObjectOld.(*unstructured.Unstructured)
			if !isUnstructured {
				return
			}

			logger := baseLogger.WithValues(
				"eventType", watch.Modified,
				"object", obj.GetName(),
				"oldObject", oldObj.GetName(),
			)
			logger.V(1).Info("Processing reconciliation")

			if err := evHandlerFunc(watch.Modified, obj, oldObj); err != nil {
				logger.Error(err, "Error in update reconciliation")
			}
		},

		//
		DeleteFunc: func(eventObject interface{}) {
			obj, isUnstructured := eventObject.(*unstructured.Unstructured)
			if !isUnstructured {
				return
			}

			logger := baseLogger.WithValues("eventType", watch.Deleted, "object", obj.GetName())
			logger.V(1).Info("Processing reconciliation")

			if err := evHandlerFunc(watch.Deleted, obj, nil); err != nil {
				logger.Error(err, "Error in delete reconciliation")
			}
		},
	}

	_, err := r.rawInformer.AddEventHandler(handlers)
	if err != nil {
		return fmt.Errorf("error adding handling functions for events to an informer: %s", err.Error())
	}

	return nil
}

// Stop signals the informer to finish. It is safe to call it more than once
func (r *Informer) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopChan)
	})
}

func (r *Informer) Start() {
	//
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()

	//
	logger := log.FromContext(*r.dependencies.Context).
		WithValues("gvr", r.options.GVR, "namespace", r.options.Namespace)

	// Listen to cancellation of parent context and propagate stopChan
	go func() {
		select {
		case <-(*r.dependencies.Context).Done():
			logger.Info(informerContextFinishedMessage)
			r.Stop()
		case <-r.stopChan:
			logger.Info(informerKilledMessage)
			return
		}
	}()

	//
	go func() {
		logger.Info(informerStartedMessage)
		r.rawInformer.Run(r.stopChan)
		logger.Info(informerStoppedMessage)
	}()
}

// IsStarted returns whether the informer has been launched or not
func (r *Informer) IsStarted() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.started
}

// HasSynced reports whether the cache of the informer has been filled at least once
func (r *Informer) HasSynced() bool {
	if r.rawInformer == nil {
		return false
	}

	return r.rawInformer.HasSynced()
}

// GetStore returns the cache store holding the watched objects
func (r *Informer) GetStore() cache.Store {
	return r.rawInformer.GetStore()
}

// GetIndexer returns the cache indexer holding the watched objects
func (r *Informer) GetIndexer() cache.Indexer {
	return r.rawInformer.GetIndexer()
}
