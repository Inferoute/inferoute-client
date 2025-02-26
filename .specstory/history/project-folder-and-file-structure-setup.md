## SpecStory

## Project Folder and File Structure Setup (12/02/2025, 11:21:46)

_**User**_

can you create this folder structure only folder and files. don't ad anything into the files.

/provider-client
├── cmd
│   └── main.go         // Entry point: sets up routes, health endpoints, and inference handlers.
├── pkg
│   ├── health
│   │   ├── collector.go   // Collects GPU stats, utilization, and Cloudflare tunnel URL.
│   │   ├── handler.go     // HTTP handler for the /v1/health endpoint.
│   │   └── reporter.go    // Sends health updates to the central node.
│   ├── inference
│   │   └── handler.go     // Forwards inference requests to the local inference engine.
│   ├── hmac
│   │   └── validator.go   // Validates HMACs on incoming requests if necessary.
│   └── util
│       ├── config.go      // Loads configuration (timeouts, intervals, tunnel settings, etc.).
│       └── logger.go      // Custom logging wrappers.
├── Dockerfile           // Builds the provider client image, including cloudflared.
├── docker-compose.yml   // (Optional) For local testing.
└── README.md            // Instructions for building, configuring, and deploying the provider client.

---

_**Assistant**_

Here's how to create this folder structure using shell commands:

```bash
mkdir -p provider-client/cmd
touch provider-client/cmd/main.go

mkdir -p provider-client/pkg/health
touch provider-client/pkg/health/collector.go
touch provider-client/pkg/health/handler.go
touch provider-client/pkg/health/reporter.go

mkdir -p provider-client/pkg/inference
touch provider-client/pkg/inference/handler.go

mkdir -p provider-client/pkg/hmac
touch provider-client/pkg/hmac/validator.go

mkdir -p provider-client/pkg/util
touch provider-client/pkg/util/config.go
touch provider-client/pkg/util/logger.go

touch provider-client/Dockerfile
touch provider-client/docker-compose.yml
touch provider-client/README.md
```

You can run these commands directly in your terminal to create the empty folder structure and files.

---

_**User**_

Please add the folder structure to @go.mdc as part of our rules

---

_**Assistant**_



---



---

