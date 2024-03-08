# GCP Artifact Registry Docker Proxy

When trying to use a GCP Artifact Registry repository as a cache for Docker Hub, Docker presents a handful of challenges:

1. No authentication is provided.
1. The request paths for library images (say `hello-world`) don't match what GCP requires.

This proxy handles both of those by using [GCP Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials) for injecting authentication and rewriting matching paths.

You can either pass the arguments via the command line as flags:

```bash
gcp-artifact-registry-docker-proxy --listen localhost:1234 --registry https://us-docker.pkg.dv/example-project/example-repo
```

Or via environment variables:

```bash
PROXY_LISTEN=localhost:1234 PROXY_REGISTRY=https://us-docker.pkg.dv/example-project/example-repo gcp-artifact-registry-docker-proxy
```
