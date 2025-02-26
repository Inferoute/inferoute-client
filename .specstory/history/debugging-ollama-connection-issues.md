## SpecStory

## Debugging Ollama Connection Issues (26/02/2025, 11:09:29)

_**User**_

Plese read @technical.md and the codebase to firust understand what you are working with.

The client starts fine, but it errors:
2025/02/26 11:11:23 Starting server on 0.0.0.0:8080
2025/02/26 11:11:23 Failed to send initial health report: failed to get models from Ollama: request failed with status code: 404

I can confirm I can get the local models using this API call -  curl http://localhost:11434/v1/models
{"object":"list","data":[{"id":"deepseek-r1:8b","object":"model","created":1737981121,"owned_by":"library"},{"id":"deepseek-r1:32b","object":"model","created":1737496831,"owned_by":"library"},{"id":"llama3.2:latest","object":"model","created":1737486178,"owned_by":"library"},{"id":"research-phi3:latest","object":"model","created":1732538838,"owned_by":"library"},{"id":"phi3:3.8b-mini-128k-instruct-fp16","object":"model","created":1732535952,"owned_by":"library"}]}


My config has been set to connect to localhost:11434 as the ollma provider

---

_**Assistant**_

I'll help you fix the error with the health report. Let me first understand the codebase structure to identify where the issue might be.

---

_****_

<details>
            <summary>Listed current directory • **12** results</summary>
        
| Name |
|-------|
| 📄 `.git` |
| 📄 `config.yaml` |
| 📄 `go.mod` |
| 📄 `go.sum` |
| 📄 `README.md` |
| 📄 `config.yaml.example` |
| 📄 `pkg` |
| 📄 `documentation` |
| 📄 `cmd` |
| 📄 `go.mdc` |
| 📄 `.cursor` |
| 📄 `.specstory` |

</details>

---

_****_



Let's look at the config file to understand the current configuration:

---

_****_

Read file: config.yaml

---

_****_



Now let's look at the code that's responsible for fetching models from Ollama:

---

_****_

<details>
            <summary>Searched codebase "get models from Ollama" • **25** results</summary>
        
| File | Lines |
|------|-------|
| `pkg/ollama/client.go` | L1-71 |
| `pkg/ollama/client.go` | L71-136 |
| `pkg/health/reporter.go` | L97-123 |
| `pkg/health/reporter.go` | L40-97 |
| `pkg/server/server.go` | L234-266 |
| `pkg/server/server.go` | L187-234 |
| `pkg/config/config.go` | L1-66 |
| `pkg/server/server.go` | L1-69 |
| `pkg/server/server.go` | L142-187 |
| `pkg/server/server.go` | L97-142 |
| `config.yaml` | L1-19 |
| `config.yaml.example` | L1-19 |
| `README.md` | L1-74 |
| `pkg/health/reporter.go` | L1-40 |
| `documentation/technical.md` | L1-33 |
| `documentation/technical.md` | L33-105 |
| `documentation/nvidia-smi.xml` | L39-67 |
| `go.mdc` | L1-80 |
| `pkg/gpu/monitor.go` | L1-70 |
| `.specstory/history/.what-is-this.md` | L1-54 |
| `go.mod` | L1-9 |
| `pkg/server/server.go` | L69-97 |
| `documentation/nvidia-smi.xml` | L1-39 |
| `cmd/main.go` | L1-85 |
| `documentation/nvidia-smi.xml` | L131-153 |

</details>

---

_****_



Now let's look at the main file to understand how the application is initialized:

---

_****_

<details>
            <summary>Listed directory cmd • **1** results</summary>
        
| Name |
|-------|
| 📄 `main.go` |

</details>

---

_****_





---

_****_

Read file: cmd/main.go

---

_****_



Let's check the Ollama client implementation to understand how it's making the API call:

---

_****_

Read file: pkg/ollama/client.go

---

_****_



I've identified the issue. The error is occurring because the client is trying to access `/api/models` endpoint, but based on your curl command, the correct endpoint for Ollama is `/v1/models`.

Let's fix the `ListModels` function in the Ollama client:

---

_****_

```diff
  func (c *Client) ListModels(ctx context.Context) (*ListModelsResponse, error) {
  	// Create request
- 	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/models", c.baseURL), nil)
+ 	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/models", c.baseURL), nil)
  	if err != nil {
  		return nil, fmt.Errorf("failed to create request: %w", err)
```

---

_****_



Now we need to run the client to see if the fix works:

---

_****_



---

_****_



---

_**User**_

we have a new error now - 2025/02/26 11:15:28 Failed to send initial health report: health report failed with status code: 500

When connecting to teh health endpoint we need to authenticate with our API key as following - --header 'Authorization: Bearer test_key_tier2b' \

The API key is what is on config.yml

---

_**Assistant**_

I'll fix the authentication issue with the health report. The error is occurring because we need to properly authenticate with the API key when sending the health report to the central system.

Let's check the health reporter implementation and update it to use the correct API key:

---

_****_

Read file: pkg/health/reporter.go

---

_****_

Read file: config.yaml

---

_****_



Let's update the config.yaml file to use the correct API key:

---

_****_

```diff
  provider:
    # Your provider API key from the Inferoute platform
-   api_key: "your-provider-api-key"
+   api_key: "test_key_tier2b"
    # URL of the central Inferoute system
    url: "http://localhost:80"
```

---

_****_





Now let's check if the health reporter is correctly using the API key from the config. Looking at the code, I can see that the health reporter is already setting the Authorization header correctly:

```go
// Add headers
req.Header.Set("Content-Type", "application/json")
req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.config.Provider.APIKey))
```

Let's run the client again to see if the fix works:

---

_****_



---

_****_



---



---

