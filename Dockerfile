# Build the manager binary.
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION} AS builder

WORKDIR /app

# The separate go mod download layer utilizes docker caching for the go modules
COPY go.mod go.sum /app/
RUN go mod download

COPY . /app/

ENV CGO_ENABLED=0
RUN go build -o /app/build/bin/manager ./cmd

# Run stage
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /app/build/bin/manager /app/bin/manager

USER 65532:65532

ENTRYPOINT ["/app/bin/manager"]
