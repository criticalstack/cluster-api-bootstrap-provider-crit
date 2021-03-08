module github.com/criticalstack/cluster-api-bootstrap-provider-crit

go 1.14

require (
	github.com/criticalstack/crit v1.0.9
	github.com/go-logr/logr v0.1.0
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/pkg/errors v0.9.1
	k8s.io/api v0.18.5
	k8s.io/apimachinery v0.18.5
	k8s.io/apiserver v0.18.4
	k8s.io/client-go v0.18.5
	k8s.io/cluster-bootstrap v0.18.3
	k8s.io/klog v1.0.0
	k8s.io/utils v0.0.0-20200603063816-c1c6865ac451
	sigs.k8s.io/cluster-api v0.3.6
	sigs.k8s.io/controller-runtime v0.6.1
)
