# Build the manager binary
FROM golang:1.19.3 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager cmd/manager/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM alpine:3.17
RUN apk --update add  postgresql-client=13.8-r0 --repository=http://dl-cdn.alpinelinux.org/alpine/v3.13/main
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/config/ /config/
USER 65532:65532

ENTRYPOINT ["/manager"]
