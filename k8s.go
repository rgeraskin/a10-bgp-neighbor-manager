package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

type Neighbors struct {
	ctx       context.Context
	clientset *kubernetes.Clientset
	a10       *A10
	label     string
}

type InformerManager interface {
	StartInformer()
	add(obj interface{})
	update(_ interface{}, obj interface{})
	delete(obj interface{})
}

// add adds a new node to the A10 device.
// It first checks if the node is eligible, and if so,
// adds the node to the A10 device.
func (n *Neighbors) add(obj interface{}) {
	node := obj.(*v1.Node)
	logger := logger.With(
		"node", node.Name,
	)
	logger.Info("Node add event")
	eligible, address := nodeEligible(node, n.label)
	if eligible {
		logger.Info("Node should be added")
		if err := n.a10.AddNeighbor(address, node.Name); err != nil {
			logger.Error("Error adding neighbor to A10:", "error", err)
		}
	}
}

// update updates a node in the A10 device.
// It first checks if the node is eligible, and if so,
// adds the node to the A10 device.
// If the node is not eligible, it removes the node from the A10 device.
func (n *Neighbors) update(_ interface{}, obj interface{}) {
	node := obj.(*v1.Node)
	logger := logger.With(
		"node", node.Name,
	)
	logger.Info("Node update event")
	eligible, address := nodeEligible(node, n.label)
	if eligible {
		logger.Info("Node should be added")
		if err := n.a10.AddNeighbor(address, node.Name); err != nil {
			logger.Error("Error adding neighbor to A10:", "error", err)
		}
	} else {
		logger.Info("Node should be removed")
		if err := n.a10.RemoveNeighbor(nodeExternalAddress(node), node.Name); err != nil {
			logger.Error("Error removing neighbor from A10:", "error", err)
		}
	}
}

// delete deletes a node from the A10 device.
// It first checks if the node is labeled, and if so,
// removes the node from the A10 device.
func (n *Neighbors) delete(obj interface{}) {
	node := obj.(*v1.Node)
	logger := logger.With(
		"node", node.Name,
	)
	logger.Info("Node delete event")
	if nodeLabeled(node, n.label) {
		logger.Info("Node should be removed")
		if err := n.a10.RemoveNeighbor(nodeExternalAddress(node), node.Name); err != nil {
			logger.Error("Error removing neighbor from A10:", "error", err)
		}
	}
}

// StartInformer starts the informer.
// It creates the shared informer factory and uses the client to connect to
// Kubernetes.
func (n *Neighbors) StartInformer() {
	// Create the shared informer factory and use the client to connect to
	// Kubernetes
	factory := informers.NewSharedInformerFactory(n.clientset, 10*time.Minute)

	// Get the informer for the right resource, in this case a Node
	informer := factory.Core().V1().Nodes().Informer()

	// Kubernetes serves an utility to handle API crashes
	defer runtime.HandleCrash()

	// This is the part where your custom code gets triggered based on the
	// event that the shared informer catches
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		// When a new node gets created
		AddFunc: n.add,
		// When a node gets updated
		UpdateFunc: n.update,
		// When a node gets deleted
		DeleteFunc: n.delete,
	})
	// You need to start the informer, in my case, it runs in the background
	go informer.Run(n.ctx.Done())

	if !cache.WaitForCacheSync(n.ctx.Done(), informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	<-n.ctx.Done()
}

// nodeEligible checks if a node is eligible to be added to the A10 device.
// It first checks if the node is ready, not cordoned, has an external address,
// and is labeled.
// Returns true if the node is eligible, false otherwise.
func nodeEligible(node *v1.Node, label string) (bool, string) {
	logger := logger.With(
		"node", node.Name,
	)
	logger.Debug("Checking node eligibility")
	eligible := false
	address := nodeExternalAddress(node)
	if nodeReady(node) && !nodeCordoned(node) && address != "" &&
		nodeLabeled(node, label) {
		eligible = true
	}
	logger.Info("Node eligible to add to A10", "eligible", eligible)
	return eligible, address
}

// nodeReady checks if a node is ready.
// It first checks if the node is ready, and if so,
// returns true. Else, it returns false.
func nodeReady(node *v1.Node) bool {
	logger := logger.With(
		"node", node.Name,
	)
	logger.Debug("Checking node readiness")
	ready := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == "Ready" {
			ready = condition.Status == v1.ConditionTrue
		}
	}
	logger.Info("Node readiness", "ready", ready)
	return ready
}

// nodeCordoned checks if a node is cordoned.
// It first checks if the node is cordoned, and if so,
// returns true. Else, it returns false.
func nodeCordoned(node *v1.Node) bool {
	cordoned := node.Spec.Unschedulable
	logger := logger.With(
		"node", node.Name,
	)
	logger.Info("Node cordoned", "cordoned", cordoned)
	return cordoned
}

// nodeLabeled checks if a node is labeled.
// It first checks if the node is labeled, and if so,
// returns true. Else, it returns false.
func nodeLabeled(node *v1.Node, label string) bool {
	logger := logger.With(
		"label", label,
		"node", node.Name,
	)
	// split label into key and value
	parts := strings.Split(label, "=")
	if len(parts) != 2 {
		logger.Error("Invalid label format")
		return false
	}
	key := parts[0]
	value := parts[1]
	logger.Debug("Node labels", "labels", node.Labels)
	labeled := node.Labels[key] == value
	logger.Info("Node labeled", "key", key, "value", value, "labeled", labeled)
	return labeled
}

// nodeExternalAddress gets the external address of a node.
// It first checks if the node has an external address, and if so,
// returns the external address. Else, it returns an empty string.
func nodeExternalAddress(node *v1.Node) string {
	logger := logger.With(
		"name", node.Name,
	)
	logger.Debug("Getting node external address")
	for _, address := range node.Status.Addresses {
		if address.Type == "ExternalIP" {
			logger.Info("Node external address", "address", address.Address)
			return address.Address
		}
	}
	logger.Debug("Node external address not found")
	return ""
}

// getKubernetesClient creates the Kubernetes client.
func getKubernetesClient() (*kubernetes.Clientset, error) {
	var config *rest.Config
	var err error
	logger.Info("Getting Kubernetes client")

	// Detect if running inside a Kubernetes cluster or using kubeconfig
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		// Load kubeconfig file for out-of-cluster use
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("error loading kubeconfig: %w", err)
		}
	} else {
		// Use in-cluster configuration
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("error creating in-cluster config: %w", err)
		}
	}

	// Create a new Kubernetes client using the in-cluster config
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error creating Kubernetes client: %w", err)
	}
	return clientset, nil
}

type KubeNodes struct {
	clientset *kubernetes.Clientset
	label     string
	Nodes     []string
}

type KubeNodesManager interface {
	GetNodes() error
}

// GetNodes gets the nodes from the Kubernetes cluster.
// It first gets the nodes from the Kubernetes cluster, and then
// checks if the nodes are eligible.
// Returns an error if the operation fails.
func (n *KubeNodes) GetNodes() error {
	logger.Info("Getting nodes from k8s")

	nodes, err := n.clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: n.label,
	})
	if err != nil {
		return fmt.Errorf("error fetching nodes: %w", err)
	}

	// Find nodes that are ready, not drained and have an external address
	// They are bgp neighbors
	for _, node := range nodes.Items {
		logger.Debug("Checking node", "name", node.Name)
		eligible, address := nodeEligible(&node, n.label)
		if eligible {
			n.Nodes = append(n.Nodes, address)
		}
	}
	return nil
}
