FROM golang:1.18-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with xcaddy
RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
RUN xcaddy build --with github.com/yourusername/caddy-headless-proxy=.

FROM alpine:3.16

# Install Chrome dependencies
RUN apk add --no-cache \
    chromium \
    nss \
    freetype \
    freetype-dev \
    harfbuzz \
    ca-certificates \
    ttf-freefont \
    nodejs \
    yarn

# Set Chrome environment variables
ENV CHROME_BIN=/usr/bin/chromium-browser \
    CHROME_PATH=/usr/lib/chromium/ \
    PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true \
    PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium-browser

# Create a non-root user to run Caddy
RUN addgroup -S caddy && \
    adduser -S -G caddy caddy

# Copy the Caddy binary from the builder stage
COPY --from=builder /build/caddy /usr/bin/caddy

# Create necessary directories with proper permissions
RUN mkdir -p /config/caddy /data/caddy /etc/caddy /var/log/caddy && \
    chown -R caddy:caddy /config/caddy /data/caddy /etc/caddy /var/log/caddy

# Copy Caddyfile
COPY Caddyfile.example /etc/caddy/Caddyfile

# Set up volumes
VOLUME ["/config/caddy", "/data/caddy", "/var/log/caddy"]

# Expose ports
EXPOSE 80 443 2019

# Switch to the non-root user
USER caddy

# Set the entrypoint
ENTRYPOINT ["/usr/bin/caddy"]
CMD ["run", "--config", "/etc/caddy/Caddyfile"]
