module github.com/k8ssandra/medusa-operator

go 1.15

require (
	github.com/bombsimon/logrusr v1.1.0
	github.com/go-logr/logr v0.4.0
	github.com/google/uuid v1.1.2
	github.com/k8ssandra/cass-operator v1.8.0
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/tools v0.1.7 // indirect
	google.golang.org/grpc v1.38.0
	google.golang.org/protobuf v1.26.0
	k8s.io/api v0.21.4
	k8s.io/apiextensions-apiserver v0.21.4 // indirect
	k8s.io/apimachinery v0.21.4
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kubernetes v1.21.4
	sigs.k8s.io/controller-runtime v0.9.2
)

replace (
	k8s.io/api => k8s.io/api v0.21.4
	k8s.io/apiserver => k8s.io/apiserver v0.21.4
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.21.4
	k8s.io/client-go => k8s.io/client-go v0.21.4
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.21.4
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.21.4
	k8s.io/code-generator => k8s.io/code-generator v0.21.5-rc.0
	k8s.io/component-base => k8s.io/component-base v0.21.4
	k8s.io/cri-api => k8s.io/cri-api v0.21.5-rc.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.21.4
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.21.4
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.21.4
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.21.4
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.21.4
	k8s.io/kubectl => k8s.io/kubectl v0.21.4
	k8s.io/kubelet => k8s.io/kubelet v0.21.4
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.21.4
	k8s.io/metrics => k8s.io/metrics v0.21.4
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.21.4
)

replace k8s.io/component-helpers => k8s.io/component-helpers v0.21.4

replace k8s.io/controller-manager => k8s.io/controller-manager v0.21.4

replace k8s.io/mount-utils => k8s.io/mount-utils v0.21.5-rc.0

replace k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.21.4

replace k8s.io/sample-controller => k8s.io/sample-controller v0.21.4

replace k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.21.4

replace k8s.io/apimachinery => k8s.io/apimachinery v0.21.5-rc.0
