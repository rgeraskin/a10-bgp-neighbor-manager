package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
)

type authResponse struct {
	AuthResponse struct {
		Signature string `json:"signature"`
	} `json:"authresponse"`
}

type ipv4Neighbor struct {
	NeighborIPV4 string `json:"neighbor-ipv4"`
	RemoteAS     int    `json:"nbr-remote-as"`
}

type ipv4Neighbors struct {
	Ipv4NeighborList []ipv4Neighbor `json:"ipv4-neighbor-list"`
}

type A10 struct {
	signature                       string
	address, username, password, as string
	remoteAS                        int
	neighbors                       []string
}

func (a *A10) login() error {
	logger.Debug("Logging in to A10")

	url := fmt.Sprintf("%s/axapi/v3/auth", a.address)

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
		logger.Fatalf("Error marshaling JSON: %v", err)
	}
	// Create a new HTTP POST request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		logger.Fatal("Error creating request to A10 to get neighbors:", err)
	}

	// make http request
	body := makeRequest(req, a.signature)

	// get signature
	var response authResponse
	if err = json.Unmarshal(body, &response); err != nil {
		logger.Fatal("Error unmarshaling JSON from A10 to get neighbors:", err)
	}
	a.signature = response.AuthResponse.Signature
	logger.Debugf("Logged in to A10, signature: %s", a.signature)
	return nil
}

func (a *A10) getNeighbors() {
	logger.Debug("Getting neighbors from A10")
	a.login()

	url := fmt.Sprintf("%s/axapi/v3/router/bgp/%s/neighbor/ipv4-neighbor", a.address, a.as)

	// Create a new HTTP GET request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		logger.Fatal("Error creating request to A10 to get neighbors:", err)
	}

	body := makeRequest(req, a.signature)

	// Parse the JSON response
	var response ipv4Neighbors
	if err = json.Unmarshal(body, &response); err != nil {
		logger.Fatal("Error unmarshaling JSON from A10 to get neighbors:", err)
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
}

func (a *A10) containsNeighbor(neighborIP string) bool {
	// a.getNeighbors()
	contains := slices.Contains(a.neighbors, neighborIP)
	logger.Debug("Checking if neighbor is in A10", "neighbor", neighborIP, "contains", contains)
	return contains
}

func (a *A10) AddNeighbor(neighborIP string) {
	if a.containsNeighbor(neighborIP) {
		logger.Info("Neighbor already exists in A10", "neighbor", neighborIP)
		return
	}
	a.login()
	logger.Info("Adding neighbor to A10", "neighbor", neighborIP)

	url := fmt.Sprintf("%s/axapi/v3/router/bgp/%s/neighbor/ipv4-neighbor/", a.address, a.as)

	// Initialize the data structure correctly
	data := map[string]interface{}{
		"ipv4-neighbor": ipv4Neighbor{
			NeighborIPV4: neighborIP,
			RemoteAS:     a.remoteAS,
		},
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		logger.Fatal("Error marshaling request data:", err)
	}
	logger.Debugf("Request body to add neighbor: %s", string(jsonData))

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Fatal("Error creating request to A10 to add neighbor:", err)
	}

	logger.Debug("Making request to A10 to add neighbor")
	_ = makeRequest(req, a.signature)

	a.neighbors = append(a.neighbors, neighborIP)
}

func (a *A10) RemoveNeighbor(neighborIP string) {
	if !a.containsNeighbor(neighborIP) {
		logger.Info("Neighbor does not exist in A10", "neighbor", neighborIP)
		return
	}
	a.login()
	logger.Info("Removing neighbor from A10", "neighbor", neighborIP)

	// Create a new HTTP DELETE request
	url := fmt.Sprintf(
		"%s/axapi/v3/router/bgp/%s/neighbor/ipv4-neighbor/%s",
		a.address,
		a.as,
		neighborIP,
	)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		logger.Fatal("Error creating request to A10 to remove neighbor:", err)
	}

	logger.Debug("Making request to A10 to remove neighbor")
	_ = makeRequest(req, a.signature)

	// Delete neighbor from A10
	idx := slices.Index(a.neighbors, neighborIP)
	a.neighbors = slices.Delete(a.neighbors, idx, idx+1)
	logger.Debug("Neighbors after deletion", "neighbors", a.neighbors)
}

func makeRequest(req *http.Request, signature string) []byte {
	// add headers
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("A10 %s", signature))

	// Create custom HTTP client with TLS skip verify
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	// make http request
	resp, err := client.Do(req)
	if err != nil {
		logger.Fatal("Error making http request:", err)
	}
	defer resp.Body.Close()

	// check if status code is ok
	if resp.StatusCode != http.StatusOK {
		logger.Fatal("HTTP request failed:", resp.StatusCode)
	}

	// Read response body into string
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Fatal("Error reading response body:", err)
	}

	// return body bytes
	return body
}

func removeExtraNeighbors(a10 *A10, kubeNodes *KubeNodes) {
	// Remove neighbors from A10 that are not in k8s
	logger.Info("Removing extra neighbors from A10")
	for _, neighbor := range a10.neighbors {
		logger.Debug("Checking neighbor", "address", neighbor)
		if !slices.Contains(kubeNodes.Nodes, neighbor) {
			logger.Info("A10 neighbor not found in k8s", "neighbor", neighbor)
			a10.RemoveNeighbor(neighbor)
		}
	}
}
