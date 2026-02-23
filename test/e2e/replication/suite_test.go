/*
Copyright 2024 The Kubernetes-CSI-Addons Authors.

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

package replication

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
)

// Cluster names for full-DR mode (when DR1_CONTEXT and DR2_CONTEXT are both set).
const (
	ClusterDR1 = "dr1"
	ClusterDR2 = "dr2"
)

var (
	cfg                *rest.Config
	k8sClient          client.Client
	k8sClientDR1       client.Client
	k8sClientDR2       client.Client
	useExistingCluster bool
	dr1Context         string
	dr2Context         string
)

func TestReplicationE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Replication E2E Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	_, _ = fmt.Fprintf(GinkgoWriter, "[Replication E2E] BeforeSuite: checking USE_EXISTING_CLUSTER\n")
	useExistingCluster = os.Getenv("USE_EXISTING_CLUSTER") == "true"
	if !useExistingCluster {
		Skip("Replication E2E suite requires USE_EXISTING_CLUSTER=true. Use make test-replication-e2e or hack/run-replication-e2e.sh")
	}

	_, _ = fmt.Fprintf(GinkgoWriter, "[Replication E2E] BeforeSuite: loading kubeconfig (KUBECONFIG=%s)\n", os.Getenv("KUBECONFIG"))
	_, _ = fmt.Fprintf(GinkgoWriter, "[Replication E2E] BeforeSuite: registering replication API in scheme\n")
	err := replicationv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	dr1Context = os.Getenv("DR1_CONTEXT")
	dr2Context = os.Getenv("DR2_CONTEXT")
	fullDR := dr1Context != "" && dr2Context != ""

	if fullDR {
		_, _ = fmt.Fprintf(GinkgoWriter, "[Replication E2E] BeforeSuite: full-DR mode DR1_CONTEXT=%q DR2_CONTEXT=%q\n", dr1Context, dr2Context)
		cfg, err = restConfigForContext(dr1Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())
		cfgDR2, err := restConfigForContext(dr2Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfgDR2).NotTo(BeNil())
		_, _ = fmt.Fprintf(GinkgoWriter, "[Replication E2E] BeforeSuite: creating Kubernetes client for DR1\n")
		k8sClientDR1, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "[Replication E2E] BeforeSuite: creating Kubernetes client for DR2\n")
		k8sClientDR2, err = client.New(cfgDR2, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		k8sClient = k8sClientDR1
	} else {
		cfg, err = restConfig()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())
		_, _ = fmt.Fprintf(GinkgoWriter, "[Replication E2E] BeforeSuite: creating Kubernetes client (single cluster)\n")
		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())
	}

	_, _ = fmt.Fprintf(GinkgoWriter, "[Replication E2E] BeforeSuite: ready\n")
})

// restConfig builds a rest.Config using the same rules as kubectl: KUBECONFIG
// env var if set, otherwise the default kubeconfig path (~/.kube/config).
// This works with minikube profiles and any context selected via KUBECONFIG
// or the default config.
func restConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}

// restConfigForContext returns rest.Config for the given context name (same kubeconfig, different context).
func restConfigForContext(contextName string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}

// GetK8sClient returns the primary Kubernetes client (DR1 in full-DR mode, else the default cluster).
func GetK8sClient() client.Client {
	return k8sClient
}

// GetK8sClientForCluster returns the client for the given cluster name when running in full-DR mode
// (DR1_CONTEXT and DR2_CONTEXT both set). Cluster name must be ClusterDR1 or ClusterDR2.
// If not in full-DR mode, returns the single shared client for any cluster name.
func GetK8sClientForCluster(clusterName string) client.Client {
	if k8sClientDR1 != nil && k8sClientDR2 != nil {
		switch clusterName {
		case ClusterDR1:
			return k8sClientDR1
		case ClusterDR2:
			return k8sClientDR2
		}
	}
	return k8sClient
}

// IsFullDRMode returns true when DR1_CONTEXT and DR2_CONTEXT are both set.
func IsFullDRMode() bool {
	return dr1Context != "" && dr2Context != ""
}

// DR1Context returns the DR1 context name (empty if not set).
func DR1Context() string { return dr1Context }

// DR2Context returns the DR2 context name (empty if not set).
func DR2Context() string { return dr2Context }
