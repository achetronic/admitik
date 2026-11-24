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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"net/http"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
	"github.com/admitikcore/admitik/internal/action"
	"github.com/admitikcore/admitik/internal/cascade"
	"github.com/admitikcore/admitik/internal/compliance"
	"github.com/admitikcore/admitik/internal/controller"
	"github.com/admitikcore/admitik/internal/controller/background"
	"github.com/admitikcore/admitik/internal/controller/clustergenerationpolicy"
	"github.com/admitikcore/admitik/internal/controller/clustermutationpolicy"
	"github.com/admitikcore/admitik/internal/controller/clustervalidationpolicy"
	"github.com/admitikcore/admitik/internal/controller/sources"
	"github.com/admitikcore/admitik/internal/evaluator"
	"github.com/admitikcore/admitik/internal/globals"
	"github.com/admitikcore/admitik/internal/informer"
	"github.com/admitikcore/admitik/internal/kubeapply"
	policyStore "github.com/admitikcore/admitik/internal/policystore"
	"github.com/admitikcore/admitik/internal/server/admission"
	"github.com/admitikcore/admitik/internal/telemetry"
	"github.com/admitikcore/admitik/internal/template"
	"github.com/admitikcore/admitik/internal/template/cel"
	"github.com/admitikcore/admitik/internal/template/gotmpl"
	"github.com/admitikcore/admitik/internal/template/plain"
	"github.com/admitikcore/admitik/internal/template/starlark"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var err error

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool

	// Custom flags from here
	var sourcesTimeToResyncInformers time.Duration

	var webhooksClientHostname string
	var webhooksClientPort int
	var webhooksClientTimeout int

	var webhooksServerPort int
	var webhooksServerPath string
	var webhooksServerCA string
	var webhooksServerCertificate string
	var webhooksServerPrivateKey string

	var webhooksServerAutogenerateCerts bool
	var webhooksServerCertsSecretName string

	var kubeClientQps float64
	var kubeClientBurst int

	var enableSpecialLabels bool
	var excludeAdmissionSelfNamespace bool
	var excludedAdmissionNamespaces string

	var complianceScanInterval time.Duration

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. "+
		"Use the port :8080. If not set, it will be 0 in order to disable the metrics server")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	// Custom flags from here
	flag.DurationVar(&sourcesTimeToResyncInformers, "sources-time-to-resync-informers", 60*time.Second,
		"Interval to resynchronize all resources in the informers")

	flag.StringVar(&webhooksClientHostname, "webhook-client-hostname", "webhooks.admitik.svc",
		"The hostname used by Kubernetes when calling the webhooks server")
	flag.IntVar(&webhooksClientPort, "webhook-client-port", 10250,
		"The port used by Kubernetes when calling the webhooks server")
	flag.IntVar(&webhooksClientTimeout, "webhook-client-timeout", 10,
		"The time waited by Kubernetes when calling the webhooks server before considering timeout")

	flag.IntVar(&webhooksServerPort, "webhook-server-port", 10250,
		"The port where the webhooks server listens")
	flag.StringVar(&webhooksServerPath, "webhook-server-path", "/admission",
		"The path where the webhooks server listens")
	flag.StringVar(&webhooksServerCA, "webhook-server-ca", "",
		"The CA bundle to use for the webhooks server")
	flag.StringVar(&webhooksServerCertificate, "webhook-server-certificate", "",
		"The Certificate used by webhooks server")
	flag.StringVar(&webhooksServerPrivateKey, "webhook-server-private-key", "",
		"The Private Key used by webhooks server")

	flag.BoolVar(&webhooksServerAutogenerateCerts, "webhook-server-autogenerate-certs", false,
		"Enable autogeneration of certificates for webhooks server")
	flag.StringVar(&webhooksServerCertsSecretName, "webhook-server-certs-secret-name", "",
		"Kubernetes Secret object name to get certificates from. Keys are: ca.crt, tls.crt, tls.key")

	flag.Float64Var(&kubeClientQps, "kube-client-qps", 5.0,
		"The QPS rate of communication between controller and the API Server")
	flag.IntVar(&kubeClientBurst, "kube-client-burst", 10,
		"The burst capacity of communication between the controller and the API Server")

	// Exclusion related flags
	flag.BoolVar(&enableSpecialLabels, "enable-special-labels", false,
		"Enable labels that perform sensitive actions")
	flag.BoolVar(&excludeAdmissionSelfNamespace, "exclude-admission-self-namespace", false,
		"Exclude Admitik resources from admission evaluations")
	flag.StringVar(&excludedAdmissionNamespaces, "excluded-admission-namespaces", "",
		"Comma-separated list of namespaces to be excluded from admission evaluations. Commonly used for 'kube-system'")

	flag.DurationVar(&complianceScanInterval, "compliance-scan-interval", 0,
		"Interval between compliance scans over the objects that already exist. Zero disables them")

	// Ref: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/log/zap@v0.21.0#Options.BindFlags
	// Production by default: JSON at info level, which is what a log pipeline reads.
	// '--zap-devel' switches to the console encoder and the debug level.
	// Only a fatal error carries a stack trace: the errors of the policies already say why
	opts := zap.Options{
		Development:     false,
		StacktraceLevel: zapcore.FatalLevel,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Define the context separated as it will be used by our custom controller too.
	// This will synchronize goroutine death when the main controller is killed
	globals.Application.Context = ctrl.SetupSignalHandler()

	// Create and store raw Kubernetes clients from client-go
	// They are used by non kubebuilder processes and controllers
	globals.Application.KubeRawClient,
		globals.Application.KubeRawCoreClient,
		globals.Application.KubeDiscoveryClient,
		err = globals.NewKubernetesClient(&rest.Config{
		QPS:   float32(kubeClientQps),
		Burst: kubeClientBurst,
	})
	if err != nil {
		setupLog.Error(err, "unable to set up kubernetes clients")
		os.Exit(1)
	}

	currentNamespace, err := globals.GetCurrentNamespace()
	if err != nil {
		setupLog.Error(err, "unable to get current namespace")
		os.Exit(1)
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:           webhookServer,
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "9ee19594.admitik.dev",
		LeaderElectionNamespace: currentNamespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// The webhook server needs a certificate: the user can give it as files, as a Secret, or let
	// Admitik generate one when the Secret does not exist
	webhooksServerCA, webhooksServerCertificate, webhooksServerPrivateKey, err = setupCertificates(certificateOptions{
		CAFile:          webhooksServerCA,
		CertificateFile: webhooksServerCertificate,
		PrivateKeyFile:  webhooksServerPrivateKey,
		SecretName:      webhooksServerCertsSecretName,
		Namespace:       currentNamespace,
		Autogenerate:    webhooksServerAutogenerateCerts,
		Hostname:        webhooksClientHostname,
	})
	if err != nil {
		setupLog.Error(err, "unable to set up the certificates of the webhook server")
		os.Exit(1)
	}

	// Load the CA bundle if defined
	caBundleBytes := []byte{}
	if webhooksServerCA != "" {
		caBundleBytes, err = os.ReadFile(webhooksServerCA)
		if err != nil {
			setupLog.Error(err, "unable to load CA bundle")
			os.Exit(1)
		}
	}

	// Generate WebhooksClientConfig with data passed by the user
	cfgWebhooksClientPort := webhooksServerPort
	if webhooksClientPort != 0 {
		cfgWebhooksClientPort = webhooksClientPort
	}
	webhookClientConfig, err := controller.GetWebhookClientConfig(caBundleBytes,
		webhooksClientHostname, cfgWebhooksClientPort, webhooksServerPath)
	if err != nil {
		setupLog.Error(err, "failed generating webhooks client config: %s", err.Error())
		os.Exit(1)
	}

	webhookClientConfigValidation, webhookClientConfigMutation := controller.GetSpecificWebhookClientConfigs(
		webhookClientConfig,
		admission.AdmissionServerValidationPath,
		admission.AdmissionServerMutationPath)

	// Create the template engines that will be used to render conditions, patches and generated objects
	engines, err := newEngines()
	if err != nil {
		setupLog.Error(err, "unable to set up template engines")
		os.Exit(1)
	}

	// Metrics about what the policies decide. They are served by the metrics endpoint
	telemetryRecorder := telemetry.NewRecorder()

	// Create registries managers that will be used by several controllers
	clusterValidationPolicyReg := policyStore.NewPolicyStore[*v1alpha1.ClusterValidationPolicy]()
	clusterMutationPolicyReg := policyStore.NewPolicyStore[*v1alpha1.ClusterMutationPolicy]()
	clusterGenerationPolicyReg := policyStore.NewPolicyStore[*v1alpha1.ClusterGenerationPolicy]()

	// One pool of informers shared by every controller: a resource type is watched once per pod
	informersPool := informer.NewPool(
		&globals.Application.Context,
		globals.Application.KubeRawClient,
		sourcesTimeToResyncInformers)

	// Evaluator used by every policy type to prepare its context and check its conditions
	policyEvaluator := evaluator.New(engines, informersPool)

	// Action that renders and writes the objects of the ClusterGenerationPolicy resources
	generateAction := action.NewGenerate(action.GenerateDependencies{
		Policies:  clusterGenerationPolicyReg,
		Evaluator: policyEvaluator,
		Engines:   engines,
		Writer:    kubeapply.NewWriter(globals.Application.KubeRawClient, mgr.GetRESTMapper()),
		Metrics:   telemetryRecorder,
	})

	// Init internal registries controllers
	// Following controllers manage internal registries for user-facing resources.
	// IMPORTANT: All the replicas are able to process and leader is not chosen for this.
	// TODO: Create a common controller to update all the registries at once (deduplicate code)
	if err = (&clustergenerationpolicy.ClusterGenerationPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		Options: clustergenerationpolicy.ClusterGenerationPolicyControllerOptions{},
		Dependencies: clustergenerationpolicy.ClusterGenerationPolicyControllerDependencies{
			ClusterGenerationPolicyRegistry: clusterGenerationPolicyReg,
			Cascade: cascade.New(globals.Application.KubeRawClient,
				globals.Application.KubeDiscoveryClient),
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterGenerationPolicy")
		os.Exit(1)
	}

	mutationPolicyReconciler := &clustermutationpolicy.ClusterMutationPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		Options: clustermutationpolicy.ClusterMutationPolicyControllerOptions{
			CurrentNamespace:              currentNamespace,
			EnableSpecialLabels:           enableSpecialLabels,
			ExcludeAdmissionSelfNamespace: excludeAdmissionSelfNamespace,
			ExcludedAdmissionNamespaces:   excludedAdmissionNamespaces,

			WebhookClientConfig: webhookClientConfigMutation,
			WebhookTimeout:      webhooksClientTimeout,
			Elected:             mgr.Elected(),
		},
		Dependencies: clustermutationpolicy.ClusterMutationPolicyControllerDependencies{
			ClusterMutationPolicyRegistry: clusterMutationPolicyReg,
		},
	}
	if err = mutationPolicyReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterMutationPolicy")
		os.Exit(1)
	}

	validationPolicyReconciler := &clustervalidationpolicy.ClusterValidationPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		Options: clustervalidationpolicy.ClusterValidationPolicyControllerOptions{
			CurrentNamespace:              currentNamespace,
			EnableSpecialLabels:           enableSpecialLabels,
			ExcludeAdmissionSelfNamespace: excludeAdmissionSelfNamespace,
			ExcludedAdmissionNamespaces:   excludedAdmissionNamespaces,

			WebhookClientConfig: webhookClientConfigValidation,
			WebhookTimeout:      webhooksClientTimeout,
			Elected:             mgr.Elected(),
		},
		Dependencies: clustervalidationpolicy.ClusterValidationPolicyControllerDependencies{
			ClusterValidationPolicyRegistry: clusterValidationPolicyReg,
		},
	}
	if err = validationPolicyReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterValidationPolicy")
		os.Exit(1)
	}

	// The webhook configurations are only written by the leader, so a replica that takes over has
	// to write them with the policies this replica already knows about
	if err = mgr.Add(controller.ElectionRefresh{
		Name: "webhook-configurations",
		Refresh: func(ctx context.Context) error {
			if err := mutationPolicyReconciler.SyncWebhookConfiguration(ctx); err != nil {
				return err
			}

			return validationPolicyReconciler.SyncWebhookConfiguration(ctx)
		},
	}); err != nil {
		setupLog.Error(err, "failed adding the webhook refresh to the manager")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	// Init BackgroundController.
	// It watches the resources expressed in the 'watchedResources' section of the policies and hands
	// their events to the actions that asked for them
	backgroundController := background.Controller{
		Options: background.Options{
			InformerDurationToResync: sourcesTimeToResyncInformers,
		},
		Dependencies: background.Dependencies{
			Context:                         &globals.Application.Context,
			ClusterGenerationPolicyRegistry: clusterGenerationPolicyReg,
			Pool:                            informersPool,
			Processors: map[string]background.Processor{
				background.ObserverTypeClusterGenerationPolicies: generateAction,
			},
		},
	}
	if err = mgr.Add(&backgroundController); err != nil {
		setupLog.Error(err, "failed adding the background controller to the manager")
		os.Exit(1)
	}

	// Init SourcesController.
	// This controller is in charge of launching watchers to cache sources expressed in some CRs in background.
	// This way we avoid retrieving them from Kubernetes on each request done by other controllers
	// such as AdmissionServer or BackgroundController.
	// IMPORTANT: All the replicas are able to process and leader is not chosen for this.
	sourcesController := sources.SourcesController{
		Options: sources.SourcesControllerOptions{
			InformerDurationToResync: sourcesTimeToResyncInformers,
		},
		Dependencies: sources.SourcesControllerDependencies{
			Context: &globals.Application.Context,

			// Every policy store whose 'sources' section must be watched. Add new policy types here
			PolicyStores: []policyStore.SourceProvider{
				clusterGenerationPolicyReg,
				clusterMutationPolicyReg,
				clusterValidationPolicyReg,
			},

			Pool: informersPool,
		},
	}
	if err = mgr.Add(&sourcesController); err != nil {
		setupLog.Error(err, "failed adding sources controller to manager")
		os.Exit(1)
	}

	// Init AdmissionServer to process incoming validation/mutation events.
	// IMPORTANT: All the replicas are able to process and leader is not chosen for this.
	admissionServer := admission.NewAdmissionServer(
		admission.AdmissionServerOptions{
			//
			ServerAddr: "0.0.0.0",
			ServerPort: webhooksServerPort,
			ServerPath: webhooksServerPath,

			//
			TLSCertificate: webhooksServerCertificate,
			TLSPrivateKey:  webhooksServerPrivateKey,
		},
		admission.AdmissionServerDependencies{
			Context:                         &globals.Application.Context,
			ClusterValidationPolicyRegistry: clusterValidationPolicyReg,
			ClusterMutationPolicyRegistry:   clusterMutationPolicyReg,
			Evaluator:                       policyEvaluator,
			Engines:                         engines,
			Metrics:                         telemetryRecorder,
		})
	if err = mgr.Add(admissionServer); err != nil {
		setupLog.Error(err, "failed adding admission server controller to manager")
		os.Exit(1)
	}

	// Init the compliance scans. They evaluate the validation policies over the existing objects
	// and publish the outcome as metrics. Disabled when the interval is zero
	if complianceScanInterval > 0 {
		complianceScanner := compliance.NewScanner(compliance.ScannerOptions{}, compliance.ScannerDependencies{
			Client:    mgr.GetClient(),
			Dynamic:   globals.Application.KubeRawClient,
			Evaluator: policyEvaluator,
		})

		runner := compliance.Runner{
			Interval: complianceScanInterval,
			Scanner:  complianceScanner,
			Metrics:  telemetryRecorder,
		}
		if err = mgr.Add(runner); err != nil {
			setupLog.Error(err, "failed adding the compliance scanner to the manager")
			os.Exit(1)
		}
	}

	// Publish how many policies are loaded, so losing all of them is visible without a request
	policyCollector := telemetry.Collector{
		Interval: 30 * time.Second,
		Recorder: telemetryRecorder,
		Counters: map[telemetry.Kind]telemetry.PolicyCounter{
			telemetry.KindValidate: clusterValidationPolicyReg.Policies,
			telemetry.KindMutate:   clusterMutationPolicyReg.Policies,
			telemetry.KindGenerate: clusterGenerationPolicyReg.Policies,
		},
	}
	if err = mgr.Add(policyCollector); err != nil {
		setupLog.Error(err, "failed adding the policy metrics collector to the manager")
		os.Exit(1)
	}

	//
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	// A replica that has not synced its caches would evaluate the policies with empty sources,
	// so it must not receive admission requests yet
	if err := mgr.AddReadyzCheck("readyz", func(_ *http.Request) error {
		return sourcesController.Ready()
	}); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	//
	setupLog.Info("starting manager")
	if err := mgr.Start(globals.Application.Context); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// newEngines builds the registry with every available template engine
func newEngines() (*template.Registry, error) {

	celExpression, err := cel.NewExpression()
	if err != nil {
		return nil, err
	}

	return template.NewRegistry(
		celExpression,
		cel.NewPlaceholders(celExpression),
		gotmpl.New(),
		starlark.New(),
		plain.New(),
	), nil
}
