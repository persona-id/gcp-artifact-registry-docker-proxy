FROM alpine:3.19.1

RUN apk --no-cache add ca-certificates

COPY gcp-artifact-registry-docker-proxy /usr/local/bin

ENTRYPOINT ["/usr/local/bin/gcp-artifact-registry-docker-proxy"]
