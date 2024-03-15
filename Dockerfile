FROM scratch
COPY gcp-artifact-registry-docker-proxy /
ENTRYPOINT ["/gcp-artifact-registry-docker-proxy"]
