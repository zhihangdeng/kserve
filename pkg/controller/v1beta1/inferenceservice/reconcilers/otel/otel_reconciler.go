/*
Copyright 2025 The KServe Authors.

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

package otel

import (
	"context"
	"fmt"

	"github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	"github.com/kserve/kserve/pkg/utils"

	"k8s.io/apimachinery/pkg/api/equality"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	otelv1beta1 "github.com/open-telemetry/opentelemetry-operator/apis/v1beta1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ProcessorResourcedetectionEnv = "resourcedetection/env"
	ProcessorTransform            = "transform"
	ProcessorFilterMetrics        = "filter/metrics"
	JobNameOtelCollector          = "otel-collector"
	PrometheusReceiver            = "prometheus"
	OtlpExporter                  = "otlp"
	ModeSidecar                   = "sidecar"

	AnnotationPrometheusPort = "prometheus.kserve.io/port"
	DefaultPrometheusPort    = "8080"

	ResourcedetectionDetectorEnv = "env"
	ResourcedetectionTimeout     = "2s"
	ResourcedetectionOverride    = false
	TransformContextDatapoint    = "datapoint"
	StatementSetNamespace        = "set(attributes[\"namespace\"], resource.attributes[\"k8s.namespace.name\"])"
	StatementSetDeployment       = "set(attributes[\"deployment\"], resource.attributes[\"k8s.deployment.name\"])"
	StatementSetPod              = "set(attributes[\"pod\"], resource.attributes[\"k8s.pod.name\"])"

	MatchTypeStrict = "strict"
	PipelineMetrics = "metrics"
	CompressionNone = "none"
	TlsKey          = "tls"
	TlsInsecureKey  = "insecure"
	EndpointKey     = "endpoint"

	KeyDetectors        = "detectors"
	KeyTimeout          = "timeout"
	KeyOverride         = "override"
	KeyMetricStatements = "metric_statements"
	KeyContext          = "context"
	KeyStatements       = "statements"
	KeyMetrics          = "metrics"
	KeyInclude          = "include"
	KeyMatchType        = "match_type"
	KeyMetricNames      = "metric_names"
	KeyConfig           = "config"
	KeyScrapeConfigs    = "scrape_configs"
	KeyJobName          = "job_name"
	KeyScrapeInterval   = "scrape_interval"
	KeyStaticConfigs    = "static_configs"
	KeyTargets          = "targets"
	KeyCompression      = "compression"
	KeyTls              = "tls"
	KeyInsecure         = "insecure"
	KeyEndpoint         = "endpoint"
)

var log = logf.Log.WithName("OTelReconciler")

type OtelReconciler struct {
	client        client.Client
	scheme        *runtime.Scheme
	OTelCollector *otelv1beta1.OpenTelemetryCollector
}

func NewOtelReconciler(client client.Client,
	scheme *runtime.Scheme,
	componentMeta metav1.ObjectMeta,
	metricNames []string,
	otelConfig v1beta1.OtelCollectorConfig,
) (*OtelReconciler, error) {
	return &OtelReconciler{
		client:        client,
		scheme:        scheme,
		OTelCollector: createOtelCollector(componentMeta, metricNames, otelConfig),
	}, nil
}

func createOtelCollector(componentMeta metav1.ObjectMeta,
	metricNames []string,
	otelConfig v1beta1.OtelCollectorConfig,
) *otelv1beta1.OpenTelemetryCollector {
	port, ok := componentMeta.Annotations[AnnotationPrometheusPort]
	if !ok {
		log.Info(fmt.Sprintf("Annotation %s is missing, using default value %s to configure OTel Collector", AnnotationPrometheusPort, DefaultPrometheusPort))
		port = DefaultPrometheusPort
	}

	processors := map[string]interface{}{
		ProcessorResourcedetectionEnv: map[string]interface{}{
			KeyDetectors: []interface{}{ResourcedetectionDetectorEnv},
			KeyTimeout:   ResourcedetectionTimeout,
			KeyOverride:  ResourcedetectionOverride,
		},
		ProcessorTransform: map[string]interface{}{
			KeyMetricStatements: []interface{}{
				map[string]interface{}{
					KeyContext: TransformContextDatapoint,
					KeyStatements: []interface{}{
						StatementSetNamespace,
						StatementSetDeployment,
						StatementSetPod,
					},
				},
			},
		},
	}

	pipelineProcessors := []string{ProcessorResourcedetectionEnv, ProcessorTransform}

	// Add filter processor to include all specified metrics
	if len(metricNames) > 0 {
		processors[ProcessorFilterMetrics] = map[string]interface{}{
			KeyMetrics: map[string]interface{}{
				KeyInclude: map[string]interface{}{
					KeyMatchType:   MatchTypeStrict,
					KeyMetricNames: metricNames,
				},
			},
		}
		pipelineProcessors = append(pipelineProcessors, ProcessorFilterMetrics)
	}

	otelCollector := &otelv1beta1.OpenTelemetryCollector{
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentMeta.Name,
			Namespace:   componentMeta.Namespace,
			Annotations: componentMeta.Annotations,
		},
		Spec: otelv1beta1.OpenTelemetryCollectorSpec{
			Mode: ModeSidecar,
			Config: otelv1beta1.Config{
				Receivers: otelv1beta1.AnyConfig{Object: map[string]interface{}{
					PrometheusReceiver: map[string]interface{}{
						KeyConfig: map[string]interface{}{
							KeyScrapeConfigs: []interface{}{
								map[string]interface{}{
									KeyJobName:        JobNameOtelCollector,
									KeyScrapeInterval: otelConfig.ScrapeInterval,
									KeyStaticConfigs: []interface{}{
										map[string]interface{}{
											KeyTargets: []interface{}{"localhost:" + port},
										},
									},
								},
							},
						},
					},
				}},
				Exporters: otelv1beta1.AnyConfig{Object: map[string]interface{}{
					OtlpExporter: map[string]interface{}{
						KeyEndpoint:    otelConfig.MetricReceiverEndpoint,
						KeyCompression: CompressionNone,
						KeyTls: map[string]interface{}{
							KeyInsecure: true,
						},
					},
				}},
				Processors: &otelv1beta1.AnyConfig{Object: processors},
				Service: otelv1beta1.Service{
					Pipelines: map[string]*otelv1beta1.Pipeline{
						PipelineMetrics: {
							Receivers:  []string{PrometheusReceiver},
							Processors: pipelineProcessors,
							Exporters:  []string{OtlpExporter},
						},
					},
				},
			},
		},
	}

	return otelCollector
}

func semanticOtelCollectorEquals(desired, existing *otelv1beta1.OpenTelemetryCollector) bool {
	return equality.Semantic.DeepEqual(desired.Spec, existing.Spec)
}

func (o *OtelReconciler) Reconcile(ctx context.Context) error {
	desired := o.OTelCollector

	existing := &otelv1beta1.OpenTelemetryCollector{}
	getExistingErr := o.client.Get(ctx, types.NamespacedName{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	}, existing)
	otelIsNotFound := apierr.IsNotFound(getExistingErr)
	if getExistingErr != nil && !otelIsNotFound {
		return fmt.Errorf("failed to get existing OTel Collector resource: %w", getExistingErr)
	}

	// ISVC is stopped, delete the httproute if it exists, otherwise, do nothing
	forceStopRuntime := utils.GetForceStopRuntime(desired)
	if (getExistingErr != nil && otelIsNotFound) && forceStopRuntime {
		return nil
	}

	if forceStopRuntime {
		if existing.GetDeletionTimestamp() == nil { // check if the otel was already deleted
			log.Info("Deleting OpenTelemetry Collector", "namespace", existing.Namespace, "name", existing.Name)
			if err := o.client.Delete(ctx, existing); err != nil {
				return err
			}
		}
		return nil
	}

	// Create or update the otel to match the desired state
	if getExistingErr != nil && otelIsNotFound {
		log.Info("Creating OTel Collector resource", "name", desired.Name)
		if err := o.client.Create(ctx, desired); err != nil {
			log.Error(err, "Failed to create OTel Collector resource", "name", desired.Name)
			return err
		}
		return nil
	}

	// Set ResourceVersion which is required for update operation.
	desired.ResourceVersion = existing.ResourceVersion
	if !semanticOtelCollectorEquals(desired, existing) {
		log.Info("Updating OTel Collector resource", "name", desired.Name)
		if err := o.client.Update(ctx, desired); err != nil {
			log.Error(err, "Failed to update OTel Collector", "name", desired.Name)
		}
	}
	return nil
}

func (o *OtelReconciler) SetControllerReferences(owner metav1.Object, scheme *runtime.Scheme) error {
	return controllerutil.SetControllerReference(owner, o.OTelCollector, scheme)
}
