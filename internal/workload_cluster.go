package internal

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/cluster-api/util"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// WorkloadCluster defines all behaviors necessary to upgrade kubernetes on a workload cluster
type WorkloadCluster interface {
	// Basic health and status checks.
	ClusterStatus(ctx context.Context) (ClusterStatus, error)
	ControlPlaneIsHealthy(ctx context.Context) (HealthCheckResult, error)
}

// Workload defines operations on workload clusters.
type Workload struct {
	Client ctrlclient.Client
	etcd   *tls.Config
}

func (w *Workload) getControlPlaneNodes(ctx context.Context) (*corev1.NodeList, error) {
	nodes := &corev1.NodeList{}
	if err := w.Client.List(ctx, nodes, ctrlclient.MatchingLabels(map[string]string{
		"node-role.kubernetes.io/master": "",
	})); err != nil {
		return nil, err
	}
	return nodes, nil
}

// HealthCheckResult maps nodes that are checked to any errors the node has related to the check.
type HealthCheckResult map[string]error

// controlPlaneIsHealthy does a best effort check of the control plane components the kubeadm control plane cares about.
// The return map is a map of node names as keys to error that that node encountered.
// All nodes will exist in the map with nil errors if there were no errors for that node.
func (w *Workload) ControlPlaneIsHealthy(ctx context.Context) (HealthCheckResult, error) {
	controlPlaneNodes, err := w.getControlPlaneNodes(ctx)
	if err != nil {
		return nil, err
	}

	response := make(map[string]error)
	for _, node := range controlPlaneNodes.Items {
		name := node.Name
		response[name] = nil

		if err := checkNodeNoExecuteCondition(node); err != nil {
			response[name] = err
			continue
		}

		apiServerPodKey := ctrlclient.ObjectKey{
			Namespace: metav1.NamespaceSystem,
			Name:      staticPodName("kube-apiserver", name),
		}
		apiServerPod := corev1.Pod{}
		if err := w.Client.Get(ctx, apiServerPodKey, &apiServerPod); err != nil {
			response[name] = err
			continue
		}
		response[name] = checkStaticPodReadyCondition(apiServerPod)

		controllerManagerPodKey := ctrlclient.ObjectKey{
			Namespace: metav1.NamespaceSystem,
			Name:      staticPodName("kube-controller-manager", name),
		}
		controllerManagerPod := corev1.Pod{}
		if err := w.Client.Get(ctx, controllerManagerPodKey, &controllerManagerPod); err != nil {
			response[name] = err
			continue
		}
		response[name] = checkStaticPodReadyCondition(controllerManagerPod)
	}

	return response, nil
}

// ClusterStatus holds stats information about the cluster.
type ClusterStatus struct {
	// Nodes are a total count of nodes
	Nodes int32
	// ReadyNodes are the count of nodes that are reporting ready
	ReadyNodes int32
}

// ClusterStatus returns the status of the cluster.
func (w *Workload) ClusterStatus(ctx context.Context) (ClusterStatus, error) {
	status := ClusterStatus{}

	// count the control plane nodes
	nodes, err := w.getControlPlaneNodes(ctx)
	if err != nil {
		return status, err
	}

	for _, node := range nodes.Items {
		nodeCopy := node
		status.Nodes++
		if util.IsNodeReady(&nodeCopy) {
			status.ReadyNodes++
		}
	}
	return status, nil
}

func staticPodName(component, nodeName string) string {
	return fmt.Sprintf("%s-%s", component, nodeName)
}

func checkStaticPodReadyCondition(pod corev1.Pod) error {
	found := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			found = true
		}
		if condition.Type == corev1.PodReady && condition.Status != corev1.ConditionTrue {
			return errors.Errorf("static pod %s/%s is not ready", pod.Namespace, pod.Name)
		}
	}
	if !found {
		return errors.Errorf("pod does not have ready condition: %v", pod.Name)
	}
	return nil
}

func checkNodeNoExecuteCondition(node corev1.Node) error {
	for _, taint := range node.Spec.Taints {
		if taint.Key == corev1.TaintNodeUnreachable && taint.Effect == corev1.TaintEffectNoExecute {
			return errors.Errorf("node has NoExecute taint: %v", node.Name)
		}
	}
	return nil
}
