# Caddy Headless Browser Proxy

A production-ready Caddy module that implements a reverse proxy using a headless browser. This module acts as a gateway between clients and target servers, optimizing communication by using a headless browser to render and process content.

## Features

- **Headless Browser Rendering**: Uses Chrome/Chromium via Rod to render pages
- **HTTP Method Support**: Handles GET, POST, PUT, DELETE, and PATCH requests
- **Header Forwarding**: Selectively forward request headers to the target
- **Cookie Management**: Forward and receive cookies between client and target
- **Resource Optimization**: Optimize HTML, CSS, JS, and images
- **Browser Pool**: Efficiently manages a pool of browser instances
- **Caching**: Configurable response caching for improved performance
- **Timeout Control**: Set timeouts for browser operations
- **User-Agent Customization**: Set custom User-Agent for the headless browser

## Installation

To build Caddy with this module:

```bash
xcaddy build --with github.com/yourusername/caddy-headless-proxy
```

## Caddyfile Syntax

```
example.com {
    headless_proxy https://target-site.com {
        timeout 30
        user_agent "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
        enable_js true
        forward_cookies true
        forward_headers Authorization X-Real-IP X-Forwarded-For
        cache_ttl 300
        max_browsers 5
        optimize_resources true
        compress_images true
        minify_content true
    }
}
```

## Directive Options

| Option | Description | Default |
|--------|-------------|---------|
| `timeout` | Timeout for browser operations in seconds | 30 |
| `user_agent` | User-Agent string for the headless browser | Chrome UA |
| `enable_js` | Whether to enable JavaScript | true |
| `forward_cookies` | Whether to forward cookies | false |
| `forward_headers` | Headers to forward to the target | [] |
| `cache_ttl` | Cache TTL in seconds (0 means no caching) | 0 |
| `max_browsers` | Maximum browser instances to keep in the pool | 5 |
| `optimize_resources` | Whether to optimize resources | false |
| `compress_images` | Whether to compress images | false |
| `minify_content` | Whether to minify HTML, CSS, and JS | false |

## Performance Considerations

- **Browser Pool**: The module maintains a pool of browser instances to reduce startup time
- **Caching**: Enable caching for frequently accessed pages to improve performance
- **Resource Optimization**: Enable resource optimization for better page load times
- **Memory Usage**: Each browser instance consumes memory, so adjust `max_browsers` based on your server's resources

## Security Considerations

- The module runs headless Chrome/Chromium with security flags enabled
- Consider running Caddy in a container or with limited privileges
- Be careful when forwarding sensitive headers or cookies

## Examples

### Basic Proxy

```
example.com {
    headless_proxy https://target-site.com
}
```

### Optimized Proxy with Caching

```
example.com {
    headless_proxy https://target-site.com {
        timeout 60
        cache_ttl 600
        optimize_resources true
        minify_content true
    }
}
```

### Proxy with Authentication

```
example.com {
    headless_proxy https://target-site.com {
        forward_headers Authorization X-API-Key
        forward_cookies true
    }
}
```

## Troubleshooting

- **High Memory Usage**: Reduce `max_browsers` value
- **Slow Response Times**: Increase `timeout` value or enable caching
- **Missing Content**: Ensure JavaScript is enabled if the target site requires it
- **Cookie Issues**: Enable `forward_cookies` if the target site requires authentication

## License

MIT License
```

Let's also create a `go.mod` file:

```go:go.mod
module github.com/yourusername/caddy-headless-proxy

go 1.18

require (
	github.com/caddyserver/caddy/v2 v2.6.4
	github.com/go-rod/rod v0.114.3
	go.uber.org/zap v1.24.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/caddyserver/certmagic v0.19.2 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/pprof v0.0.0-20210720184732-4bb14d4b1be1 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/libdns/libdns v0.2.1 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/mholt/acmez v1.2.0 // indirect
	github.com/miekg/dns v1.1.55 // indirect
	github.com/onsi/ginkgo/v2 v2.9.5 // indirect
	github.com/prometheus/client_golang v1.15.1 // indirect
	github.com/prometheus/client_model v0.4.0 // indirect
	github.com/prometheus/common v0.42.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	github.com/quic-go/qpack v0.4.0 // indirect
	github.com/quic-go/qtls-go1-20 v0.3.1 // indirect
	github.com/quic-go/quic-go v0.37.4 // indirect
	github.com/ysmood/fetchup v0.2.3 // indirect
	github.com/ysmood/goob v0.4.0 // indirect
	github.com/ysmood/got v0.34.1 // indirect
	github.com/ysmood/gson v0.7.3 // indirect
	github.com/ysmood/leakless v0.8.0 // indirect
	github.com/zeebo/blake3 v0.2.3 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/exp v0.0.0-20230310171629-522b1b587ee0 // indirect
	golang.org/x/mod v0.11.0 // indirect
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	golang.org/x/tools v0.10.0 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
)
```

And finally, let's create a simple example Caddyfile:

```caddyfile:Caddyfile.example
{
	order headless_proxy before reverse_proxy
}

example.com {
	headless_proxy https://target-site.com {
		timeout 30
		user_agent "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
		enable_js true
		forward_cookies true
		forward_headers Authorization X-Real-IP X-Forwarded-For
		cache_ttl 300
		max_browsers 5
		optimize_resources true
		compress_images true
		minify_content true
	}

	log {
		output file /var/log/caddy/headless-proxy.log
		format json
	}
}
