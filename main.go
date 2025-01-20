package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/log"
)

var logger *log.Logger

type Config struct {
	Address       string
	Username      string
	Password      string
	AS            int
	RemoteAS      int
	LabelSelector string
}

func (c *Config) Get() error {
	remoteAS := os.Getenv("A10_REMOTE_AS")
	if remoteAS == "" {
		return fmt.Errorf("A10_REMOTE_AS environment variable must be set")
	}
	remoteASInt, err := strconv.Atoi(remoteAS)
	if err != nil {
		return fmt.Errorf("A10_REMOTE_AS must be a number: %w", err)
	}

	// Get A10 address
	a10Address := os.Getenv("A10_ADDRESS")
	if a10Address == "" {
		return fmt.Errorf("A10_ADDRESS environment variable must be set")
	}

	// Get A10 username
	a10Username := os.Getenv("A10_USERNAME")
	if a10Username == "" {
		return fmt.Errorf("A10_USERNAME environment variable must be set")
	}

	// Get A10 password
	a10Password := os.Getenv("A10_PASSWORD")
	if a10Password == "" {
		return fmt.Errorf("A10_PASSWORD environment variable must be set")
	}

	// Get A10 AS
	a10As := os.Getenv("A10_AS")
	if a10As == "" {
		return fmt.Errorf("A10_AS environment variable must be set")
	}
	a10AsInt, err := strconv.Atoi(a10As)
	if err != nil {
		return fmt.Errorf("A10_AS must be a number: %w", err)
	}

	// Label selector for nodes
	labelSelector := os.Getenv("NODES_LABEL_SELECTOR")
	if labelSelector == "" {
		return fmt.Errorf(
			"label selector must be set with NODES_LABEL_SELECTOR environment variable",
		)
	}
	// try to split labelSelector by = and count the number of parts
	if parts := strings.Split(labelSelector, "="); len(parts) != 2 {
		return fmt.Errorf("label selector must be in the format key=value")
	}

	c.RemoteAS = remoteASInt
	c.Address = a10Address
	c.Username = a10Username
	c.Password = a10Password
	c.AS = a10AsInt
	c.LabelSelector = labelSelector

	return nil
}

func (c *Config) Log() {
	logger.Info(
		"Inputs",
		"a10Address",
		c.Address,
		"a10Username",
		c.Username,
		"a10AS",
		c.AS,
		"remoteAS",
		c.RemoteAS,
		"labelSelector",
		c.LabelSelector,
	)
	logger.Debug("Password", "a10Password", c.Password)
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

	// Setup context and graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gracefulShutdown(cancel)

	// Get configuration
	config := Config{}
	if err := config.Get(); err != nil {
		logger.Fatal("Error getting configuration:", err)
	}
	config.Log()

	// Get Kubernetes client
	clientset, err := getKubernetesClient()
	if err != nil {
		logger.Fatal("Error getting Kubernetes client:", err)
	}

	// Get A10 current neighbors
	a10 := A10{
		ctx:      ctx,
		address:  config.Address,
		username: config.Username,
		password: config.Password,
		as:       config.AS,
		remoteAS: config.RemoteAS,
	}
	if err := a10.GetNeighbors(); err != nil {
		logger.Fatal("Error getting neighbors from A10:", err)
	}

	// Get Kubernetes nodes
	kubeNodes := KubeNodes{
		clientset: clientset,
		label:     config.LabelSelector,
	}
	if err := kubeNodes.GetNodes(); err != nil {
		logger.Fatal("Error getting nodes from k8s:", err)
	}

	// Remove extra neighbors from A10 that are not in k8s
	if err := removeExtraNeighbors(&a10, &kubeNodes); err != nil {
		logger.Fatal("Error removing extra neighbors from A10:", err)
	}

	// Start informer to watch for changes in k8s
	neighbors := Neighbors{
		ctx:       ctx,
		clientset: clientset,
		label:     config.LabelSelector,
		a10:       &a10,
	}
	neighbors.StartInformer()
}

func gracefulShutdown(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutting down...")
		cancel()
	}()
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
