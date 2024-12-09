# go-tunol

A lightweight, developer-friendly tunneling service written in Go. Similarly to Ngrok, easily expose your local services to the internet securely.

Built as a way to debug and test full stack applications on mobile devices, using existing docker-compose config as configuration. 


## Overview

go-tunol allows you to create secure tunnels to your localhost, making it easy to:
- Demo and share web applications
- Debug and test applications on mobile devices
- Test webhooks locally
- Share your local development environment

## Quick Start

```bash
# Install
go install github.com/jwtly10/go-tunol@latest

# Start a tunnel to your local service
tunol --port 3000
```

Your local service will be available at a generated URL like: `https://tunol.dev/abc123`

## Features

- Instant tunnel creation
- Secure WebSocket communication
- Low latency forwarding
- Request/response logging

## Roadmap

- Request replaying - Save and replay HTTP requests for testing
- Online request viewer - Monitor and debug requests in real-time
- Authentication and access controls
- Traffic metrics and analytics
- Automatic infra standup with docker compose configuration

## Development

```bash
# Clone the repository
git clone https://github.com/jwtly10/go-tunol.git

# Install dependencies
go mod download

# Run tests
go test ./... -v
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.