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

	configutil "github.com/criticalstack/crit/pkg/config/util"
	critv1 "github.com/criticalstack/crit/pkg/config/v1alpha2"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	ctrl "sigs.k8s.io/controller-runtime"

	bootstrapv1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha2"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal/cloudinit"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal/secret"
)

func (r *CritConfigReconciler) reconcileControlPlane(
	ctx context.Context,
	config *bootstrapv1.CritConfig,
	configOwner *bsutil.ConfigOwner,
	cluster *clusterv1.Cluster,
	cfg *critv1.ControlPlaneConfiguration,
) (ctrl.Result, error) {
	logger := r.Log.WithValues("kind", configOwner.GetKind(), "version", configOwner.GetResourceVersion(), "name", configOwner.GetName())

	// information from the cluster object is injected into the CritConfig
	if err := r.reconcileControlPlaneClusterConfiguration(config, configOwner, cluster, cfg); err != nil {
		return ctrl.Result{}, err
	}

	// get or create CA certificates
	certificates, err := r.reconcileControlPlaneCertificates(ctx, config, configOwner, cluster, cfg)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "unable to lookup or create cluster certificates")
	}

	// create a new cloud-config for the control plane
	data, err := r.reconcileControlPlaneCloudConfig(config, cfg, certificates)
	if err != nil {
		logger.Error(err, "failed to create a control plane node configuration")
		return ctrl.Result{}, err
	}

	// store cloud-config in a secret, referenced in the CritConfig status
	if err := r.storeBootstrapData(ctx, config, cluster, data); err != nil {
		logger.Error(err, "failed to store bootstrap data")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *CritConfigReconciler) reconcileControlPlaneClusterConfiguration(
	config *bootstrapv1.CritConfig,
	configOwner *bsutil.ConfigOwner,
	cluster *clusterv1.Cluster,
	cfg *critv1.ControlPlaneConfiguration,
) error {
	logger := r.Log.WithValues("critconfig", fmt.Sprintf("%s/%s", config.Namespace, config.Name))

	machine := &clusterv1.Machine{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(configOwner.Object, machine); err != nil {
		return errors.Wrapf(err, "cannot convert %s to Machine", configOwner.GetKind())
	}

	if !cluster.Spec.ControlPlaneEndpoint.IsZero() {
		if !contains(cfg.KubeAPIServerConfiguration.ExtraSANs, cluster.Spec.ControlPlaneEndpoint.Host) {
			cfg.KubeAPIServerConfiguration.ExtraSANs = append(cfg.KubeAPIServerConfiguration.ExtraSANs, cluster.Spec.ControlPlaneEndpoint.Host)
			logger.Info("appending to ExtraSan", "ExtraSANs", strings.Join(cfg.KubeAPIServerConfiguration.ExtraSANs, ", "))
		}
		if cfg.ControlPlaneEndpoint.IsZero() {
			cfg.ControlPlaneEndpoint.Host = cluster.Spec.ControlPlaneEndpoint.Host
			cfg.ControlPlaneEndpoint.Port = cluster.Spec.ControlPlaneEndpoint.Port
			logger.Info("altering ControlPlaneConfiguration", "ControlPlaneEndpoint", cfg.ControlPlaneEndpoint)
		}
	}

	// if there are no ClusterName defined in NodeConfiguration, use
	// Cluster.Name
	if cfg.ClusterName == "" {
		cfg.ClusterName = cluster.Name
		logger.Info("altering ControlPlaneConfiguration", "ClusterName", cfg.ClusterName)
	}

	// if there are no Network settings defined in NodeConfiguration, use
	// ClusterNetwork settings, (if defined)
	if cluster.Spec.ClusterNetwork != nil {
		if cfg.NodeConfiguration.KubeletConfiguration.ClusterDomain == "" && cluster.Spec.ClusterNetwork.ServiceDomain != "" {
			cfg.NodeConfiguration.KubeletConfiguration.ClusterDomain = cluster.Spec.ClusterNetwork.ServiceDomain
			logger.Info("altering ControlPlaneConfiguration", "DNSDomain", cfg.NodeConfiguration.KubeletConfiguration.ClusterDomain)
		}
		if cfg.ServiceSubnet == "" &&
			cluster.Spec.ClusterNetwork.Services != nil &&
			len(cluster.Spec.ClusterNetwork.Services.CIDRBlocks) > 0 {
			cfg.ServiceSubnet = strings.Join(cluster.Spec.ClusterNetwork.Services.CIDRBlocks, "")
			logger.Info("altering ControlPlaneConfiguration", "ServiceSubnet", cfg.ServiceSubnet)
		}
		if cfg.PodSubnet == "" &&
			cluster.Spec.ClusterNetwork.Pods != nil &&
			len(cluster.Spec.ClusterNetwork.Pods.CIDRBlocks) > 0 {
			cfg.PodSubnet = strings.Join(cluster.Spec.ClusterNetwork.Pods.CIDRBlocks, "")
			logger.Info("altering ControlPlaneConfiguration", "PodSubnet", cfg.PodSubnet)
		}
	}

	// if there are no KubernetesVersion settings defined in NodeConfiguration,
	// use Version from the config owner (if defined)
	if cfg.NodeConfiguration.KubernetesVersion == "" && machine.Spec.Version != nil {
		cfg.NodeConfiguration.KubernetesVersion = *machine.Spec.Version
		logger.Info("altering ControlPlaneConfiguration", "KubernetesVersion", cfg.NodeConfiguration.KubernetesVersion)
	}
	return config.Spec.SetConfig(cfg)
}

func contains(ss []string, match string) bool {
	for _, s := range ss {
		if s == match {
			return true
		}
	}
	return false
}

func (r *CritConfigReconciler) reconcileControlPlaneCertificates(
	ctx context.Context,
	config *bootstrapv1.CritConfig,
	configOwner *bsutil.ConfigOwner,
	cluster *clusterv1.Cluster,
	cfg *critv1.ControlPlaneConfiguration,
) (secret.Certificates, error) {
	certificates := secret.NewCertificatesForControlPlane(filepath.Join(cfg.NodeConfiguration.KubeDir, "pki"))
	if err := certificates.LookupOrGenerate(
		ctx,
		r.Client,
		types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
		*metav1.NewControllerRef(config, bootstrapv1.GroupVersion.WithKind("CritConfig")),
	); err != nil {
		return nil, errors.Wrap(err, "unable to lookup or create cluster certificates")
	}
	return certificates, nil
}

func (r *CritConfigReconciler) reconcileControlPlaneCloudConfig(
	config *bootstrapv1.CritConfig,
	cfg *critv1.ControlPlaneConfiguration,
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
	cloudConfig.Files = append(cloudConfig.Files, certificates.AsFiles()...)
	return cloudinit.Write(cloudConfig)
}
