package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type Endpoint struct {
	Path        string            `json:"path"`
	Method      string            `json:"method"`
	StatusCode  int               `json:"statusCode"`
	Response    any               `json:"response"`
	Headers     map[string]string `json:"headers"`
	Summary     string            `json:"summary,omitempty"`
	Description string            `json:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
}

type Config struct {
	Endpoints []Endpoint `json:"endpoints"`
	Info      *OpenAPIInfo `json:"info,omitempty"`
}

type OpenAPIInfo struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

// extractPathParams extracts parameter names from path like /api/users/{id}/posts/{postId}
func extractPathParams(path string) []string {
	re := regexp.MustCompile(`\{([^}]+)\}`)
	matches := re.FindAllStringSubmatch(path, -1)
	params := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			params = append(params, match[1])
		}
	}
	return params
}

func generateOpenAPISpec(config *Config) map[string]any {
	info := map[string]any{
		"title":   "Stubber API",
		"version": "1.0.0",
	}
	if config.Info != nil {
		if config.Info.Title != "" {
			info["title"] = config.Info.Title
		}
		if config.Info.Description != "" {
			info["description"] = config.Info.Description
		}
		if config.Info.Version != "" {
			info["version"] = config.Info.Version
		}
	}

	paths := make(map[string]any)

	for _, ep := range config.Endpoints {
		method := strings.ToLower(ep.Method)
		if method == "" {
			method = "get"
		}

		statusCode := ep.StatusCode
		if statusCode == 0 {
			statusCode = 200
		}

		operation := map[string]any{
			"responses": map[string]any{
				statusCodeToString(statusCode): map[string]any{
					"description": "Successful response",
					"content": map[string]any{
						"application/json": map[string]any{
							"example": ep.Response,
						},
					},
				},
			},
		}

		// Extract and add path parameters
		pathParams := extractPathParams(ep.Path)
		if len(pathParams) > 0 {
			params := make([]map[string]any, 0, len(pathParams))
			for _, paramName := range pathParams {
				params = append(params, map[string]any{
					"name":     paramName,
					"in":       "path",
					"required": true,
					"schema": map[string]any{
						"type": "string",
					},
				})
			}
			operation["parameters"] = params
		}

		if ep.Summary != "" {
			operation["summary"] = ep.Summary
		}
		if ep.Description != "" {
			operation["description"] = ep.Description
		}
		if len(ep.Tags) > 0 {
			operation["tags"] = ep.Tags
		}

		if _, exists := paths[ep.Path]; !exists {
			paths[ep.Path] = make(map[string]any)
		}
		paths[ep.Path].(map[string]any)[method] = operation
	}

	// Add built-in endpoints
	paths["/health"] = map[string]any{
		"get": map[string]any{
			"summary": "Health check endpoint",
			"tags":    []string{"System"},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Service is healthy",
					"content": map[string]any{
						"text/plain": map[string]any{
							"example": "ok",
						},
					},
				},
			},
		},
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info":    info,
		"paths":   paths,
	}
}

func statusCodeToString(code int) string {
	switch code {
	case 200:
		return "200"
	case 201:
		return "201"
	case 204:
		return "204"
	case 400:
		return "400"
	case 401:
		return "401"
	case 403:
		return "403"
	case 404:
		return "404"
	case 500:
		return "500"
	default:
		return "200"
	}
}

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/config/endpoints.json"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	// Generate OpenAPI spec
	openAPISpec := generateOpenAPISpec(&config)

	// Serve OpenAPI spec
	http.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openAPISpec)
	})
	log.Println("Registered: GET /openapi.json")

	// Group endpoints by path
	pathEndpoints := make(map[string][]Endpoint)
	for _, ep := range config.Endpoints {
		pathEndpoints[ep.Path] = append(pathEndpoints[ep.Path], ep)
	}

	for path, endpoints := range pathEndpoints {
		eps := endpoints // capture for closure
		http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			// Find matching endpoint for this method
			var endpoint *Endpoint
			for i := range eps {
				if eps[i].Method == "" || eps[i].Method == r.Method {
					endpoint = &eps[i]
					break
				}
			}

			if endpoint == nil {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}

			for k, v := range endpoint.Headers {
				w.Header().Set(k, v)
			}
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/json")
			}

			statusCode := endpoint.StatusCode
			if statusCode == 0 {
				statusCode = 200
			}
			w.WriteHeader(statusCode)

			json.NewEncoder(w).Encode(endpoint.Response)
		})
		for _, ep := range eps {
			log.Printf("Registered: %s %s", ep.Method, ep.Path)
		}
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
