package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type Endpoint struct {
	Path       string            `json:"path"`
	Method     string            `json:"method"`
	StatusCode int               `json:"statusCode"`
	Response   any               `json:"response"`
	Headers    map[string]string `json:"headers"`
}

type Config struct {
	Endpoints []Endpoint `json:"endpoints"`
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
