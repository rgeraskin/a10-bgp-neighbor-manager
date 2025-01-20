name="a10-bgp-neighbor-manager"
repository="rgeraskin/{name}".format(name=name)

local_resource(
    name="go-build",
    cmd="CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/app .",
    deps=["main.go"],
)
docker_build(
    repository,
    context=".",
    dockerfile_contents="\n".join([
        "FROM scratch",
        "COPY build/app /app/app",
        'ENTRYPOINT ["/app/app"]',
    ]),
    only=[
        "./build/"
    ]
)

allow_k8s_contexts('talos-admin@talos')
k8s_yaml(helm(
    "./helm",
    name=name,
    values=["./helm/secret-values.yaml"],
))
