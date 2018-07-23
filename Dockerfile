# Image information - multi-stage image
FROM golang:1.10-alpine AS builder

# Install
RUN echo "http://dl-cdn.alpinelinux.org/alpine/edge/community" >> /etc/apk/repositories \
    && apk --update add glide git

# Create directory
RUN mkdir -p /go/src/github.com/weaveworks/prometheus-swarm/
WORKDIR /go/src/github.com/weaveworks/prometheus-swarm/

# Install deps
COPY glide.yaml /go/src/github.com/weaveworks/prometheus-swarm/
COPY glide.lock /go/src/github.com/weaveworks/prometheus-swarm/
RUN mkdir /app && glide install

# Build app
COPY ./ /go/src/github.com/weaveworks/prometheus-swarm/
RUN CGO_ENABLED=0 GOOS=linux go build \
        -a -installsuffix cgo \
        -o /app/main /go/src/github.com/weaveworks/prometheus-swarm/swarm.go

# Final image
MAINTAINER Marc Trudel <mtrudel@wizcorp.jp>
FROM alpine:3.6
COPY --from=builder /app/main /promswarm
ENTRYPOINT ["/promswarm", "discover"]
