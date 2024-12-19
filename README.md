# go-tunol

A lightweight, developer-friendly tunneling service written in Go. Similarly to Ngrok, easily expose your local services to the internet securely.

Built as a way to debug and test full stack applications on mobile devices, using existing docker-compose config as configuration. 

## Overview

go-tunol allows you to create secure tunnels to your localhost, making it easy to:
- Demo and share web applications
- Debug and test applications on mobile devices
- Test webhooks locally
- Share your local development environment

## Features (& Planned)
- ✅ Basic tunnel creation
- ⭕ Docker integration (given a docker compose file, spin up the services and create tunnels, parsing env URLs where possible)
- ⭕ Local dashboard for:
    - ⭕ Request replaying
    - ⭕ Detailed Request monitoring/viewing
    - ⭕ Traffic metrics

## Quick Start

Firstly sign in to the [admin dashboard](https://tunol.dev) to generate a new auth token.

```bash
# Install (assuming you have Go installed)
go install github.com/jwtly10/go-tunol/cmd/tunol@latest

# Once you have the auth token
tunol --login <AUTH_TOKEN>

# You can now start tunnels to your local services
tunol --port 3001 --port 8001
```

You'll be met with a CLI dashboard showing the status of your tunnels:

```bash
 go-tunol dashboard                                           11:09:51
══════════════════════════════════════════════════════
📡 TUNNELS
   https://dzxp4r8a.tunol.dev ➔ localhost:3001 (⬆️ 1s)
   https://ppeg6yxq.tunol.dev ➔ localhost:8001 (⬆️ 1s)

📊 STATS (last 60s)
   0 requests • 0 errors • 0.0% success rate
   Average response time: 0ms

LIVE TRAFFIC (newest first)
────────────────────────────────────────────────────

Press Ctrl+C to quit
```

Your local service will be available at a generated URL like: `https://<SOME_ID>.tunol.dev`

## Development

```bash
# Clone the repository
git clone https://github.com/jwtly10/go-tunol.git

# Install dependencies
go mod download

# Run tests
go test ./... -v

# To run the server
go run cmd/server/main.go

# TO run the CLI
go run cmd/tunol/main.go [args]
```

### Environment
[.env-example](.env-example) contains the environment variables required to run the server,
and also explains some of the environment variables needed to overrite defaults of the CLI

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the [MIT License](LICENSE).