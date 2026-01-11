package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
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
	Script      string            `json:"script,omitempty"`
	Context     map[string]any    `json:"context,omitempty"`
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

// JSRuntime manages a pool of goja VMs for script execution
type JSRuntime struct {
	pool sync.Pool
}

// RequestData contains all request information passed to scripts
type RequestData struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
	Params  map[string]string `json:"params"`
}

// ScriptResult contains the response from a script execution
type ScriptResult struct {
	Body       any               `json:"body"`
	StatusCode int               `json:"statusCode,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

func NewJSRuntime() *JSRuntime {
	return &JSRuntime{
		pool: sync.Pool{
			New: func() any {
				return goja.New()
			},
		},
	}
}

func (jr *JSRuntime) Execute(script string, req RequestData, context map[string]any) (*ScriptResult, error) {
	vm := jr.pool.Get().(*goja.Runtime)
	defer func() {
		// Clear the VM state before returning to pool
		vm.ClearInterrupt()
		jr.pool.Put(vm)
	}()

	// Set up built-in functions
	vm.Set("console", map[string]any{
		"log": func(args ...any) {
			log.Println("[JS]", args)
		},
	})

	// Add utility functions
	vm.Set("uuid", func() string {
		return generateUUID()
	})
	vm.Set("now", func() string {
		return time.Now().UTC().Format(time.RFC3339)
	})
	vm.Set("timestamp", func() int64 {
		return time.Now().Unix()
	})

	// Set request data
	vm.Set("req", req)
	vm.Set("request", req)

	// Set context variables
	for key, value := range context {
		vm.Set(key, value)
	}

	// Execute the script
	val, err := vm.RunString(script)
	if err != nil {
		return nil, err
	}

	// Handle the result
	result := &ScriptResult{StatusCode: 200}

	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		result.Body = nil
		return result, nil
	}

	exported := val.Export()

	// Check if result is a ScriptResult-like object with body/statusCode/headers
	if m, ok := exported.(map[string]any); ok {
		if body, exists := m["body"]; exists {
			result.Body = body
			if sc, exists := m["statusCode"]; exists {
				if code, ok := sc.(int64); ok {
					result.StatusCode = int(code)
				} else if code, ok := sc.(float64); ok {
					result.StatusCode = int(code)
				}
			}
			if headers, exists := m["headers"]; exists {
				if h, ok := headers.(map[string]any); ok {
					result.Headers = make(map[string]string)
					for k, v := range h {
						if s, ok := v.(string); ok {
							result.Headers[k] = s
						}
					}
				}
			}
			return result, nil
		}
	}

	// Otherwise, use the entire result as the body
	result.Body = exported
	return result, nil
}

func generateUUID() string {
	// Simple UUID v4 generation without external deps
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(time.Now().UnixNano() >> (i * 8))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func sprintf(format string, a ...any) string {
	// Simple hex formatter for UUID
	result := ""
	argIdx := 0
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) && format[i+1] == 'x' {
			if argIdx < len(a) {
				if b, ok := a[argIdx].([]byte); ok {
					for _, v := range b {
						result += hexChar(v>>4) + hexChar(v&0x0f)
					}
				}
				argIdx++
			}
			i++
		} else {
			result += string(format[i])
		}
	}
	return result
}

func hexChar(b byte) string {
	const hex = "0123456789abcdef"
	return string(hex[b&0x0f])
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

	// Initialize JS runtime for scripted endpoints
	jsRuntime := NewJSRuntime()

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
		pathPattern := path
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

			// Set headers from config
			for k, v := range endpoint.Headers {
				w.Header().Set(k, v)
			}
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/json")
			}

			// Check if this is a scripted endpoint
			if endpoint.Script != "" {
				// Build request data for the script
				reqData := RequestData{
					Method:  r.Method,
					Path:    r.URL.Path,
					Query:   make(map[string]string),
					Headers: make(map[string]string),
					Params:  extractPathValues(pathPattern, r.URL.Path),
				}

				// Extract query parameters
				for key, values := range r.URL.Query() {
					if len(values) > 0 {
						reqData.Query[key] = values[0]
					}
				}

				// Extract headers
				for key, values := range r.Header {
					if len(values) > 0 {
						reqData.Headers[key] = values[0]
					}
				}

				// Parse body if present
				if r.Body != nil {
					bodyBytes, err := io.ReadAll(r.Body)
					if err == nil && len(bodyBytes) > 0 {
						var bodyData any
						if json.Unmarshal(bodyBytes, &bodyData) == nil {
							reqData.Body = bodyData
						} else {
							reqData.Body = string(bodyBytes)
						}
					}
				}

				// Execute the script
				result, err := jsRuntime.Execute(endpoint.Script, reqData, endpoint.Context)
				if err != nil {
					log.Printf("Script error for %s %s: %v", r.Method, r.URL.Path, err)
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{
						"error":   "Script execution failed",
						"details": err.Error(),
					})
					return
				}

				// Set response headers from script result
				for k, v := range result.Headers {
					w.Header().Set(k, v)
				}

				// Use script's status code or endpoint's or default to 200
				statusCode := result.StatusCode
				if statusCode == 0 {
					statusCode = endpoint.StatusCode
				}
				if statusCode == 0 {
					statusCode = 200
				}
				w.WriteHeader(statusCode)

				json.NewEncoder(w).Encode(result.Body)
				return
			}

			// Static response (no script)
			statusCode := endpoint.StatusCode
			if statusCode == 0 {
				statusCode = 200
			}
			w.WriteHeader(statusCode)

			json.NewEncoder(w).Encode(endpoint.Response)
		})
		for _, ep := range eps {
			scriptIndicator := ""
			if ep.Script != "" {
				scriptIndicator = " [scripted]"
			}
			log.Printf("Registered: %s %s%s", ep.Method, ep.Path, scriptIndicator)
		}
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// extractPathValues extracts actual values from URL path based on pattern
// e.g., pattern="/api/users/{id}", path="/api/users/123" returns {"id": "123"}
func extractPathValues(pattern, path string) map[string]string {
	result := make(map[string]string)

	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")

	if len(patternParts) != len(pathParts) {
		return result
	}

	for i, part := range patternParts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			paramName := part[1 : len(part)-1]
			result[paramName] = pathParts[i]
		}
	}

	return result
}
