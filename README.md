# Stubber

An extremely lightweight mock API server that serves configurable JSON responses. Runs on just 16MB of memory.

## Quick Start

```bash
docker run -v ./config:/config -p 8080:8080 ghcr.io/serviceweave/stubber:latest
```

## Configuration

Create a `config/endpoints.json` file:

```json
{
  "endpoints": [
    {
      "path": "/api/users",
      "method": "GET",
      "statusCode": 200,
      "response": {"users": [{"id": 1, "name": "Alice"}]}
    }
  ]
}
```

## Kubernetes Deployment

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: stubber-config
data:
  endpoints.json: |
    {
      "endpoints": [
        {
          "path": "/api/users",
          "method": "GET",
          "statusCode": 200,
          "response": {"users": [{"id": 1, "name": "Alice"}]}
        }
      ]
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stubber
spec:
  replicas: 1
  selector:
    matchLabels:
      app: stubber
  template:
    metadata:
      labels:
        app: stubber
    spec:
      containers:
        - name: stubber
          image: ghcr.io/serviceweave/stubber:latest
          ports:
            - containerPort: 8080
          volumeMounts:
            - name: config
              mountPath: /config
          readinessProbe:
            httpGet:
              path: /health
              port: 8080
          resources:
            limits:
              memory: "16Mi"
              cpu: "50m"
      volumes:
        - name: config
          configMap:
            name: stubber-config
---
apiVersion: v1
kind: Service
metadata:
  name: stubber
spec:
  selector:
    app: stubber
  ports:
    - port: 80
      targetPort: 8080
```

## Endpoints

- `GET /health` - Health check
- `GET /openapi.json` - OpenAPI 3.0 specification
