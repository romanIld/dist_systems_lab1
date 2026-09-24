# Builds all Lab1 binaries on Linux and packs them into a small runtime image.

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go vet ./... \
 && CGO_ENABLED=0 go build -trimpath -o /out/ ./cmd/...

FROM alpine:3.22
WORKDIR /app
COPY --from=build /out/ /app/bin/
ENV PATH="/app/bin:${PATH}"
