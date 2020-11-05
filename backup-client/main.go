package main

import (
	"context"
	"log"

	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	err := api.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to register cassandrabackup scheme: %s", err)
	}

	cfg := config.GetConfigOrDie()

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		log.Fatalf("failed to create controller-runtime client: %s", err)
	}

	podList := &corev1.PodList{}
	if err := k8sClient.List(context.Background(), podList, client.InNamespace("medusa-operator")); err == nil {
		log.Printf("found %d pods", len(podList.Items))
	} else {
		log.Printf("failed to list pods: %s\n", err)
	}
}

