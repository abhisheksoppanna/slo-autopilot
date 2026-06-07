# Multi-stage build for the slo-autopilot CLI. Used by the GitHub Action so a
# pipeline can run the gate without a Go toolchain.
#   docker build -t slo-autopilot .
FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/slo-autopilot ./cmd/slo-autopilot

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/slo-autopilot /usr/local/bin/slo-autopilot
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/slo-autopilot"]
