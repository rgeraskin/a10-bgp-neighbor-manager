package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
)

var logger *log.Logger

type Config struct {
	RemoteAS      int
	Address       string
	Username      string
	Password      string
	AS            string
	LabelSelector string
}

func getConfig() (*Config, error) {
	remoteAS := os.Getenv("A10_REMOTE_AS")
	if remoteAS == "" {
		return nil, fmt.Errorf("A10_REMOTE_AS environment variable must be set")
	}
	remoteASInt, err := strconv.Atoi(remoteAS)
	if err != nil {
		return nil, fmt.Errorf("A10_REMOTE_AS must be a number: %w", err)
	}

	// Get A10 address
	a10Address := os.Getenv("A10_ADDRESS")
	if a10Address == "" {
		return nil, fmt.Errorf("A10_ADDRESS environment variable must be set")
	}

	// Get A10 username
	a10Username := os.Getenv("A10_USERNAME")
	if a10Username == "" {
		return nil, fmt.Errorf("A10_USERNAME environment variable must be set")
	}

	// Get A10 password
	a10Password := os.Getenv("A10_PASSWORD")
	if a10Password == "" {
		return nil, fmt.Errorf("A10_PASSWORD environment variable must be set")
	}

	// Get A10 AS
	a10As := os.Getenv("A10_AS")
	if a10As == "" {
		return nil, fmt.Errorf("A10_AS environment variable must be set")
	}

	// Label selector for nodes
	labelSelector := os.Getenv("NODES_LABEL_SELECTOR")
	if labelSelector == "" {
		return nil, fmt.Errorf(
			"label selector must be set with NODES_LABEL_SELECTOR environment variable",
		)
	}
	// try to split labelSelector by = and count the number of parts
	if parts := strings.Split(labelSelector, "="); len(parts) != 2 {
		return nil, fmt.Errorf("label selector must be in the format key=value")
	}

	return &Config{
		RemoteAS:      remoteASInt,
		Address:       a10Address,
		Username:      a10Username,
		Password:      a10Password,
		AS:            a10As,
		LabelSelector: labelSelector,
	}, nil
}

func main() {
	// Initialize logger
	level := log.InfoLevel
	if os.Getenv("DEBUG") != "" {
		level = log.DebugLevel
	}
	logger = log.NewWithOptions(os.Stderr, log.Options{
		// ReportCaller:    true,
		ReportTimestamp: true,
		Level:           level,
		// Formatter:       log.LogfmtFormatter,
	})

	clientset, err := getKubernetesClient()
	if err != nil {
		logger.Fatal("Error getting Kubernetes client:", err)
	}
	config, err := getConfig()
	if err != nil {
		logger.Fatal("Error getting configuration:", err)
	}
	logger.Info(
		"Inputs",
		"a10Address",
		config.Address,
		"a10Username",
		config.Username,
		"a10AS",
		config.AS,
		"remoteAS",
		config.RemoteAS,
		"labelSelector",
		config.LabelSelector,
	)
	logger.Debug("Password", "a10Password", config.Password)

	a10 := A10{
		address:  config.Address,
		username: config.Username,
		password: config.Password,
		as:       config.AS,
		remoteAS: config.RemoteAS,
	}
	if err := a10.getNeighbors(); err != nil {
		logger.Fatal("Error getting neighbors from A10:", err)
	}

	// Remove extra neighbors from A10 that are not in k8s
	kubeNodes := KubeNodes{
		clientset: clientset,
		label:     config.LabelSelector,
	}
	if err := kubeNodes.GetNodes(); err != nil {
		logger.Fatal("Error getting nodes from k8s:", err)
	}
	if err := removeExtraNeighbors(&a10, &kubeNodes); err != nil {
		logger.Fatal("Error removing extra neighbors from A10:", err)
	}

	neighbors := Neighbors{
		clientset: clientset,
		label:     config.LabelSelector,
		a10:       &a10,
	}
	neighbors.StartInformer()
}

// func synchronizeNeighbors(a10 *A10, neighbors *NodesNeighbor) {
// 	// Remove neighbors from A10 that are not in k8s
// 	logger.Debug("Removing extra neighbors from A10")
// 	for _, neighbor := range a10.Neighbors {
// 		logger.Debug("Checking neighbor", "address", neighbor)
// 		if !neighbors.Contains(neighbor) {
// 			logger.Debug("A10 neighbor not found in k8s", "neighbor", neighbor)
// 			a10.RemoveNeighbor(neighbor)
// 		}
// 	}
// 	// Add missing neighbors to A10
// 	logger.Debug("Adding missing neighbors to A10")
// 	for _, neighbor := range neighbors.Nodes {
// 		logger.Debug("Checking neighbor", "node", neighbor.Name, "address", neighbor.Address)
// 		if !slices.Contains(a10.Neighbors, neighbor.Address) {
// 			logger.Debug("k8s neighbor not found in A10", "neighbor", neighbor.Address)
// 			a10.AddNeighbor(neighbor.Address)
// 		}
// 	}
// }

// func getNodeAddress(node *v1.Node, addressType v1.NodeAddressType) string {
// 	for _, address := range node.Status.Addresses {
// 		if address.Type == addressType {
// 			return address.Address
// 		}
// 	}
// 	return ""
// }
