/*
Copyright 2020 Critical Stack, LLC

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

package controllers

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	configutil "github.com/criticalstack/crit/pkg/config/util"
	critv1 "github.com/criticalstack/crit/pkg/config/v1alpha2"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"

	bootstrapv1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha2"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal/cloudinit"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal/secret"
)

func (r *CritConfigReconciler) reconcileWorker(
	ctx context.Context,
	config *bootstrapv1.CritConfig,
	configOwner *bsutil.ConfigOwner,
	cluster *clusterv1.Cluster,
	cfg *critv1.WorkerConfiguration,
) (ctrl.Result, error) {
	logger := r.Log.WithValues("kind", configOwner.GetKind(), "version", configOwner.GetResourceVersion(), "name", configOwner.GetName())

	if !cluster.Status.ControlPlaneInitialized {
		logger.Info("control plane is not yet initialized")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// information from the cluster object is injected into the CritConfig
	if err := r.reconcileWorkerClusterConfiguration(config, configOwner, cluster, cfg); err != nil {
		if requeueErr, ok := errors.Cause(err).(capierrors.HasRequeueAfterError); ok {
			return ctrl.Result{RequeueAfter: requeueErr.GetRequeueAfter()}, nil
		}
		return ctrl.Result{}, err
	}

	// information from the cluster object is injected into the CritConfig
	if err := r.reconcileWorkerBootstrapToken(ctx, config, configOwner, cluster, cfg); err != nil {
		if requeueErr, ok := errors.Cause(err).(capierrors.HasRequeueAfterError); ok {
			return ctrl.Result{RequeueAfter: requeueErr.GetRequeueAfter()}, nil
		}
		return ctrl.Result{}, err
	}

	// get cluster CA certificate
	certificates, err := r.reconcileWorkerCertificates(ctx, config, configOwner, cluster, cfg)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "unable to lookup cluster certificates")
	}

	// create a new cloud-config for workers
	data, err := r.reconcileWorkerCloudConfig(config, cfg, certificates)
	if err != nil {
		logger.Error(err, "failed to create a worker node configuration")
		return ctrl.Result{}, err
	}

	// store cloud-config in a secret, referenced in the CritConfig status
	if err := r.storeBootstrapData(ctx, config, cluster, data); err != nil {
		logger.Error(err, "failed to store bootstrap data")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *CritConfigReconciler) reconcileWorkerCertificates(
	ctx context.Context,
	config *bootstrapv1.CritConfig,
	configOwner *bsutil.ConfigOwner,
	cluster *clusterv1.Cluster,
	cfg *critv1.WorkerConfiguration,
) (secret.Certificates, error) {
	certificates := secret.NewCertificatesForWorker(filepath.Join(cfg.NodeConfiguration.KubeDir, "pki"))
	if err := certificates.Lookup(
		ctx,
		r.Client,
		types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	); err != nil {
		return nil, errors.Wrap(err, "unable to lookup cluster certificates")
	}
	if err := certificates.EnsureAllExist(); err != nil {
		return nil, errors.Wrap(err, "missing certificates")
	}
	return certificates, nil
}

func (r *CritConfigReconciler) reconcileWorkerCloudConfig(
	config *bootstrapv1.CritConfig,
	cfg *critv1.WorkerConfiguration,
	certificates secret.Certificates,
) ([]byte, error) {
	data, err := configutil.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	cloudConfig := &cloudinit.Config{
		Files: append(config.Spec.Files, bootstrapv1.File{
			Path:        "/var/lib/crit/config.yaml",
			Owner:       "root:root",
			Permissions: "0640",
			Encoding:    bootstrapv1.Base64,
			Content:     base64.StdEncoding.EncodeToString(data),
		}),
		PreCritCommands:  config.Spec.PreCritCommands,
		PostCritCommands: config.Spec.PostCritCommands,
		Users:            config.Spec.Users,
		NTP:              config.Spec.NTP,
	}
	if config.Spec.Verbosity {
		cloudConfig.Verbosity = "-v"
	}
	for _, cert := range certificates.GetByPurpose(secret.ClusterCA).AsFiles() {
		if !strings.HasSuffix(cert.Path, "crt") {
			continue
		}
		cloudConfig.Files = append(cloudConfig.Files, cert)
	}
	return cloudinit.Write(cloudConfig)
}

func (r *CritConfigReconciler) reconcileWorkerClusterConfiguration(
	config *bootstrapv1.CritConfig,
	configOwner *bsutil.ConfigOwner,
	cluster *clusterv1.Cluster,
	cfg *critv1.WorkerConfiguration,
) error {
	logger := r.Log.WithValues("critconfig", fmt.Sprintf("%s/%s", config.Namespace, config.Name))

	if cfg.ControlPlaneEndpoint.IsZero() {
		if cluster.Spec.ControlPlaneEndpoint.IsZero() {
			return errors.Wrap(&capierrors.RequeueAfterError{RequeueAfter: 10 * time.Second}, "waiting for cluster controller to set Cluster.Spec.ControlPlaneEndpoint")
		}
		cfg.ControlPlaneEndpoint.Host = cluster.Spec.ControlPlaneEndpoint.Host
		cfg.ControlPlaneEndpoint.Port = cluster.Spec.ControlPlaneEndpoint.Port
		logger.Info("altering WorkerConfiguration", "ControlPlaneEndpoint", cfg.ControlPlaneEndpoint)
	}

	// if there are no ClusterName defined in NodeConfiguration, use
	// Cluster.Name
	if cfg.ClusterName == "" {
		cfg.ClusterName = cluster.Name
		logger.Info("altering WorkerConfiguration", "ClusterName", cfg.ClusterName)
	}

	// if there are no Network settings defined in NodeConfiguration, use
	// ClusterNetwork settings, (if defined)
	if cluster.Spec.ClusterNetwork != nil {
		if cfg.NodeConfiguration.KubeletConfiguration.ClusterDomain == "" && cluster.Spec.ClusterNetwork.ServiceDomain != "" {
			cfg.NodeConfiguration.KubeletConfiguration.ClusterDomain = cluster.Spec.ClusterNetwork.ServiceDomain
			logger.Info("altering WorkerConfiguration", "DNSDomain", cfg.NodeConfiguration.KubeletConfiguration.ClusterDomain)
		}
	}

	// if there are no KubernetesVersion settings defined in NodeConfiguration,
	// use Version from the config owner (if defined)
	switch configOwner.GetKind() {
	case "Machine":
		machine := &clusterv1.Machine{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(configOwner.Object, machine); err != nil {
			return errors.Wrapf(err, "cannot convert %s to Machine", configOwner.GetKind())
		}
		if cfg.NodeConfiguration.KubernetesVersion == "" && machine.Spec.Version != nil {
			cfg.NodeConfiguration.KubernetesVersion = *machine.Spec.Version
			logger.Info("altering WorkerConfiguration", "KubernetesVersion", cfg.NodeConfiguration.KubernetesVersion)
		}
	case "MachineSet":
		machineSet := &clusterv1.MachineSet{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(configOwner.Object, machineSet); err != nil {
			return errors.Wrapf(err, "cannot convert %s to MachineSet", configOwner.GetKind())
		}
		if cfg.NodeConfiguration.KubernetesVersion == "" && machineSet.Spec.Template.Spec.Version != nil {
			cfg.NodeConfiguration.KubernetesVersion = *machineSet.Spec.Template.Spec.Version
			logger.Info("altering WorkerConfiguration", "KubernetesVersion", cfg.NodeConfiguration.KubernetesVersion)
		}
	case "MachineDeployment":
		machineDeployment := &clusterv1.MachineDeployment{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(configOwner.Object, machineDeployment); err != nil {
			return errors.Wrapf(err, "cannot convert %s to MachineDeployment", configOwner.GetKind())
		}
		if cfg.NodeConfiguration.KubernetesVersion == "" && machineDeployment.Spec.Template.Spec.Version != nil {
			cfg.NodeConfiguration.KubernetesVersion = *machineDeployment.Spec.Template.Spec.Version
			logger.Info("altering WorkerConfiguration", "KubernetesVersion", cfg.NodeConfiguration.KubernetesVersion)
		}
	}

	return config.Spec.SetConfig(cfg)
}

func (r *CritConfigReconciler) reconcileWorkerBootstrapToken(
	ctx context.Context,
	config *bootstrapv1.CritConfig,
	configOwner *bsutil.ConfigOwner,
	cluster *clusterv1.Cluster,
	cfg *critv1.WorkerConfiguration,
) error {
	logger := r.Log.WithValues("kind", configOwner.GetKind(), "version", configOwner.GetResourceVersion(), "name", configOwner.GetName())

	if config.Status.Ready {
		// If the BootstrapToken has been generated for a worker and the
		// infrastructure is not ready. This indicates the token in the worker
		// config has not been consumed and it may need a refresh.
		if cfg.BootstrapToken != "" && !configOwner.IsInfrastructureReady() {
			remoteClient, err := r.remoteClientGetter(ctx, r.Client, util.ObjectKey(cluster), r.Scheme)
			if err != nil {
				logger.Error(err, "error creating remote cluster client")
				return err
			}
			logger.Info("refreshing token until the infrastructure has a chance to consume it")
			if err := refreshToken(ctx, remoteClient, cfg.BootstrapToken); err != nil {
				return errors.Wrapf(err, "failed to refresh bootstrap token")
			}
			if err := config.Spec.SetConfig(cfg); err != nil {
				return err
			}
			return errors.Wrap(&capierrors.RequeueAfterError{RequeueAfter: DefaultTokenTTL / 2}, "requeue until token is consumed")
		}
		return nil
	}

	// if BootstrapToken already contains a token, respect it, otherwise create
	// a new bootstrap token for the worker
	if cfg.BootstrapToken == "" {
		remoteClient, err := r.remoteClientGetter(ctx, r.Client, util.ObjectKey(cluster), r.Scheme)
		if err != nil {
			return err
		}
		token, err := createToken(ctx, remoteClient)
		if err != nil {
			return errors.Wrapf(err, "failed to create new bootstrap token")
		}
		logger.Info("altering WorkerConfiguration.BootstrapToken", "Token", token)
		cfg.BootstrapToken = token
	}
	return config.Spec.SetConfig(cfg)
}
