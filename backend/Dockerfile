# Parametrized multi-stage build for any backend service.
# Build with: docker build -f backend/Dockerfile --build-arg SERVICE=auth -t platform/auth backend
ARG SERVICE

FROM golang:1.23-alpine AS build
ARG SERVICE
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/service ./services/${SERVICE}/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/service /service
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/service"]
