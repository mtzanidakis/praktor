# Stage 1: Build the React UI
FROM node:24-alpine AS ui-builder
WORKDIR /ui
COPY ui/ .
RUN npm install && npm run build

# Stage 2: Build the Go binary
FROM golang:1.26-alpine AS go-builder
RUN apk add --no-cache busybox-static ca-certificates tzdata && \
   update-ca-certificates
RUN adduser \
   --disabled-password \
   --gecos "praktor user" \
   --home "/nonexistent" \
   --shell "/sbin/nologin" \
   --no-create-home \
   --uid 10321 \
   praktor
RUN egrep '^(praktor|root):' /etc/passwd > /etc/passwd.scratch && \
	egrep '^(praktor|root):' /etc/group > /etc/group.scratch
WORKDIR /src
COPY . .
RUN go mod download
COPY --from=ui-builder /ui/dist/ ./internal/web/static/
ARG VERSION
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}" -o /praktor ./cmd/praktor

# Stage 4: Minimal runtime
FROM scratch AS base
COPY --from=go-builder /bin/busybox.static /bin/sh
COPY --from=go-builder /bin/busybox.static /bin/install
COPY --from=go-builder /bin/busybox.static /bin/rm
COPY --from=go-builder /bin/busybox.static /bin/mkdir
COPY --from=go-builder /bin/busybox.static /bin/chown
COPY --from=go-builder /praktor /praktor
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=go-builder /etc/passwd.scratch /etc/passwd
COPY --from=go-builder /etc/group.scratch /etc/group
RUN mkdir -p /data/agents/global && chown -R praktor:praktor /data
RUN install -d -m 1777 /tmp && \
   rm -rf -- /bin

# Flatten final image
FROM scratch
COPY --from=base / /
USER praktor
EXPOSE 8080
ENTRYPOINT ["/praktor", "gateway"]
