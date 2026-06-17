# Build
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hivemind-agent ./cmd/agent

# Run
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hivemind-agent /usr/local/bin/hivemind-agent
ENTRYPOINT ["/usr/local/bin/hivemind-agent"]
