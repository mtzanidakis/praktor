# Stage 1: Build the React UI
FROM node:24-alpine AS ui-builder
WORKDIR /ui
COPY ui/ .
RUN npm install && npm run build

# Stage 2: Build the Go binary
FROM golang:1.26-alpine AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui-builder /ui/dist/ ./internal/web/static/
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=$(git describe --tags --always 2>/dev/null || echo docker)" -o /praktor ./cmd/praktor

# Stage 3: Minimal runtime
FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata
COPY --from=go-builder /praktor /usr/local/bin/praktor
EXPOSE 8080 4222
ENTRYPOINT ["praktor", "gateway"]
