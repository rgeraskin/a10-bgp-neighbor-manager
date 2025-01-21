package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"
)

const (
	defaultTimeout    = 10 * time.Second
	maxRequestRetries = 3
	authEndpoint      = "/axapi/v3/auth"
	bgpEndpoint       = "/axapi/v3/router/bgp/%d/neighbor/ipv4-neighbor"
)

// authResponse is the response from the A10 device when logging in.
type authResponse struct {
	AuthResponse struct {
		Signature string `json:"signature"`
	} `json:"authresponse"`
}

// ipv4Neighbor is the structure of the data for a BGP neighbor.
type ipv4Neighbor struct {
	NeighborIPV4 string `json:"neighbor-ipv4"`
	RemoteAS     int    `json:"nbr-remote-as"`
}

// ipv4Neighbors is the structure of the data for a list of BGP neighbors.
type ipv4Neighbors struct {
	Ipv4NeighborList []ipv4Neighbor `json:"ipv4-neighbor-list"`
}

type A10 struct {
	signature                   string
	address, username, password string
	remoteAS, as                int
	neighbors                   []string

	ctx    context.Context
	mu     sync.RWMutex
	client *http.Client
}

type BGPManager interface {
	AddNeighbor(neighborIP string) error
	RemoveNeighbor(neighborIP string) error
	GetNeighbors() ([]string, error)
	containsNeighbor(neighborIP string) bool
	login() error
	makeRequest(req *http.Request, signature string) ([]byte, error)
}

// AddHTTPClient adds an http client to the A10 struct.
// It creates an http client with TLS skip verify.
// To reuse the same client for multiple requests
func (a *A10) AddHTTPClient() {
	// create http client with TLS skip verify
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	a.client = &http.Client{
		Transport: tr,
		Timeout:   defaultTimeout,
	}
}

// login logs in to the A10 device.
// Returns an error if the operation fails.
func (a *A10) login() error {
	logger.Debug("Logging in to A10")

	url := fmt.Sprintf("%s%s", a.address, authEndpoint)

	// Define the structure of the data
	data := map[string]interface{}{
		"credentials": map[string]string{
			"username": a.username,
			"password": a.password,
		},
	}

	// Convert the JSON object to a string
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	// Create a new HTTP POST request
	req, err := http.NewRequestWithContext(a.ctx, "POST", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return fmt.Errorf("creating request to A10 to get neighbors: %w", err)
	}

	// make http request
	body, err := a.makeRequest(req, a.signature)
	if err != nil {
		return fmt.Errorf("making http request: %w", err)
	}

	// get signature
	var response authResponse
	if err = json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("unmarshaling JSON from A10 to get neighbors: %w", err)
	}
	a.signature = response.AuthResponse.Signature
	logger.Debugf("Logged in to A10, signature: %s", a.signature)
	return nil
}

// GetNeighbors gets the neighbors from the A10 device.
// It first logs in to the A10 device, and then
// makes a request to get the neighbors.
// Returns an error if the operation fails.
func (a *A10) GetNeighbors() error {
	logger.Debug("Getting neighbors from A10")

	// login to A10
	if err := a.login(); err != nil {
		return fmt.Errorf("logging in to A10: %w", err)
	}

	url := fmt.Sprintf("%s%s", a.address, fmt.Sprintf(bgpEndpoint, a.as))

	// Create a new HTTP GET request
	req, err := http.NewRequestWithContext(a.ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request to A10 to get neighbors: %w", err)
	}

	body, err := a.makeRequest(req, a.signature)
	if err != nil {
		return fmt.Errorf("making http request: %w", err)
	}

	// Parse the JSON response
	var response ipv4Neighbors
	if err = json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("unmarshaling JSON from A10 to get neighbors: %w", err)
	}

	// For debugging, print the response
	logger.Debug("Response from A10 to get neighbors:", "response", response)

	// Update the A10 struct's Neighbors field
	a.neighbors = []string{}
	for _, n := range response.Ipv4NeighborList {
		if n.RemoteAS == a.remoteAS {
			a.neighbors = append(a.neighbors, n.NeighborIPV4)
		}
	}
	logger.Debug(
		"Neighbors from A10 with AS that matches",
		"AS",
		a.remoteAS,
		"neighbors",
		a.neighbors,
	)
	return nil
}

// containsNeighbor checks if a neighbor exists in the A10 device.
// It first checks if the neighbor exists, and if so,
// returns true.
func (a *A10) containsNeighbor(neighborIP string) bool {
	logger := logger.With(
		"neighbor", neighborIP,
	)
	// a.getNeighbors()
	a.mu.RLock()
	defer a.mu.RUnlock()
	contains := slices.Contains(a.neighbors, neighborIP)
	logger.Debug("Checking if neighbor is in A10", "contains", contains)
	return contains
}

// AddNeighbor adds a new BGP neighbor to the A10 device.
// It first checks if the neighbor already exists, and if not,
// creates a new neighbor with the specified IP and remote AS.
// Returns an error if the operation fails.
func (a *A10) AddNeighbor(neighborIP string) error {
	logger := logger.With(
		"neighbor", neighborIP,
	)

	if a.containsNeighbor(neighborIP) {
		logger.Info("Neighbor already exists in A10")
		return nil
	}
	if err := a.login(); err != nil {
		return fmt.Errorf("logging in to A10: %w", err)
	}
	logger.Info("Adding neighbor to A10")

	url := fmt.Sprintf("%s%s", a.address, fmt.Sprintf(bgpEndpoint, a.as))

	// Initialize the data structure correctly
	data := map[string]interface{}{
		"ipv4-neighbor": ipv4Neighbor{
			NeighborIPV4: neighborIP,
			RemoteAS:     a.remoteAS,
		},
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling request data: %w", err)
	}
	logger.Debugf("Request body to add neighbor: %s", string(jsonData))

	req, err := http.NewRequestWithContext(a.ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("creating request to A10 to add neighbor: %w", err)
	}

	logger.Debug("Making request to A10 to add neighbor")
	_, err = a.makeRequest(req, a.signature)
	if err != nil {
		return fmt.Errorf("making http request: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.neighbors = append(a.neighbors, neighborIP)
	return nil
}

// RemoveNeighbor removes a BGP neighbor from the A10 device.
// It first checks if the neighbor exists, and if so,
// removes the neighbor from the A10 device.
// Returns an error if the operation fails.
func (a *A10) RemoveNeighbor(neighborIP string) error {
	logger := logger.With(
		"neighbor", neighborIP,
	)

	if !a.containsNeighbor(neighborIP) {
		logger.Info("Neighbor does not exist in A10")
		return nil
	}
	if err := a.login(); err != nil {
		return fmt.Errorf("logging in to A10: %w", err)
	}
	logger.Info("Removing neighbor from A10")

	// Create a new HTTP DELETE request
	url := fmt.Sprintf(
		"%s%s/%s",
		a.address,
		fmt.Sprintf(bgpEndpoint, a.as),
		neighborIP,
	)

	req, err := http.NewRequestWithContext(a.ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("creating request to A10 to remove neighbor: %w", err)
	}

	logger.Debug("Making request to A10 to remove neighbor")
	_, err = a.makeRequest(req, a.signature)
	if err != nil {
		return fmt.Errorf("making http request: %w", err)
	}

	// Delete neighbor from A10
	a.mu.Lock()
	defer a.mu.Unlock()
	idx := slices.Index(a.neighbors, neighborIP)
	a.neighbors = slices.Delete(a.neighbors, idx, idx+1)
	logger.Debug("Neighbors after deletion", "neighbors", a.neighbors)
	return nil
}

// makeRequest makes an http request to the A10 device.
// It adds the necessary headers to the request, and then
// makes the request.
// Returns an error if the operation fails.
func (a *A10) makeRequest(req *http.Request, signature string) ([]byte, error) {
	// add headers
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("A10 %s", signature))

	var resp *http.Response
	var lastErr error
	for i := 0; i < maxRequestRetries; i++ {
		if lastErr != nil {
			logger.Error("Retrying request", "error", lastErr, "attempt", i+1)
		}

		var err error
		resp, err = a.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		// check if status code is ok
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP request failed: %d", resp.StatusCode)
			continue
		}

		// Read response body into string
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %w", err)
		}

		return body, nil
	}

	return nil, fmt.Errorf(
		"error making http request after %d retries: %v",
		maxRequestRetries,
		lastErr,
	)
}

// removeExtraNeighbors removes neighbors from A10 that are not in k8s.
// It first gets the neighbors from A10, and then
// removes the neighbors that are not in k8s.
// Returns an error if the operation fails.
func removeExtraNeighbors(a10 *A10, kubeNodes *KubeNodes) error {
	// Remove neighbors from A10 that are not in k8s
	logger.Info("Removing extra neighbors from A10")

	// copy contents of a10.neighbors to a10Neighbors
	// because we will modify a10.neighbors
	a10Neighbors := make([]string, len(a10.neighbors))
	copy(a10Neighbors, a10.neighbors)

	logger.Debug("A10 neighbors", "neighbors", a10Neighbors)
	for _, neighbor := range a10Neighbors {
		logger.Debug("Checking neighbor", "address", neighbor)
		if !slices.Contains(kubeNodes.Nodes, neighbor) {
			logger.Info("A10 neighbor not found in k8s", "neighbor", neighbor)
			if err := a10.RemoveNeighbor(neighbor); err != nil {
				return fmt.Errorf("removing neighbor: %w", err)
			}
		}
	}
	return nil
}
