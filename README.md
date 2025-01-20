# A10 BGP Neighbor Manager

This is a Kubernetes controller to manage BGP neighbors on an A10 device.

It uses the A10 API to manage the BGP neighbors and the Kubernetes API to add eligible nodes to the A10 device and remove them if they are not eligible.

Nodes are eligible if they are:

1. labeled with the NODES_LABEL_SELECTOR label
1. are ready
1. are not cordoned
1. have an external IP address

Actually, this controller doesn't control anything in K8S. It just uses the K8S API to watch for nodes events.

## Usage

### Local

```shell
export KUBECONFIG=kubeconfig.yaml
export A10_ADDRESS=https://address
export A10_USERNAME=admin
export A10_PASSWORD=XXX
export A10_AS=12345
export A10_REMOTE_AS=54321
export NODES_LABEL_SELECTOR="bgp=cilium"
go run .
```

* If kubeconfig is not set, the tool will use the in-cluster config.
* `export DEBUG=true` will enable debug logging.

### Helm

Adjust the values in `helm/values.yaml`

```shell
helm upgrade --install a10-bgp-neighbor-controller ./helm
```

## Development

1. `mise install` to install dev dependencies
1. `go run .` to run the app locally
1. `tilt up` to deploy app to a cluster
1. `tilt down` to tear down the app
1. `mise run publish` to build and push the docker image to a registry
