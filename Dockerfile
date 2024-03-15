FROM scratch
COPY gcp-artifact-registry-docker-proxy /
CMD ["/gcp-artifact-registry-docker-proxy"]
