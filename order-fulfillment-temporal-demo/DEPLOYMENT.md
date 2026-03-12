# Deployment Guide

## Local Development

### Prerequisites
- Go 1.21+
- Docker & Docker Compose
- Make (optional)

### Setup Steps

1. **Clone and navigate to project:**
```bash
cd order-fulfillment-temporal-demo
```

2. **Install dependencies:**
```bash
go mod download
```

3. **Start Temporal and databases:**
```bash
make docker-up
# or
docker-compose -f docker/docker-compose.yml up -d
```

4. **Verify Temporal is running:**
- Temporal UI: http://localhost:8080
- Temporal gRPC: localhost:7233

5. **Run the worker:**
```bash
make run-worker
# or
go run cmd/worker/main.go
```

6. **Run the API (in another terminal):**
```bash
make run-api
# or
go run cmd/api/main.go
```

7. **Test the API:**
```bash
curl http://localhost:8080/health
```

## Docker Deployment

### Build Images

```bash
# Build API image
docker build -f docker/Dockerfile.api -t order-api:latest .

# Build Worker image
docker build -f docker/Dockerfile.worker -t order-worker:latest .
```

### Run with Docker Compose

Add to `docker-compose.yml`:

```yaml
  api:
    build:
      context: ..
      dockerfile: docker/Dockerfile.api
    ports:
      - "8080:8080"
    environment:
      - TEMPORAL_HOST_PORT=temporal:7233
    depends_on:
      - temporal
      - app-postgres

  worker:
    build:
      context: ..
      dockerfile: docker/Dockerfile.worker
    environment:
      - TEMPORAL_HOST_PORT=temporal:7233
    depends_on:
      - temporal
      - app-postgres
```

## Kubernetes Deployment

### Prerequisites
- Kubernetes cluster
- kubectl configured
- Temporal Helm chart installed

### Deploy Application

1. **Create namespace:**
```bash
kubectl create namespace order-fulfillment
```

2. **Create ConfigMap:**
```bash
kubectl create configmap order-config \
  --from-file=config.yaml \
  -n order-fulfillment
```

3. **Create Secrets:**
```bash
kubectl create secret generic order-secrets \
  --from-literal=db-password=yourpassword \
  -n order-fulfillment
```

4. **Deploy API:**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: order-api
spec:
  replicas: 3
  selector:
    matchLabels:
      app: order-api
  template:
    metadata:
      labels:
        app: order-api
    spec:
      containers:
      - name: api
        image: order-api:latest
        ports:
        - containerPort: 8080
        env:
        - name: TEMPORAL_HOST_PORT
          value: "temporal-frontend:7233"
```

5. **Deploy Worker:**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: order-worker
spec:
  replicas: 5
  selector:
    matchLabels:
      app: order-worker
  template:
    metadata:
      labels:
        app: order-worker
    spec:
      containers:
      - name: worker
        image: order-worker:latest
        env:
        - name: TEMPORAL_HOST_PORT
          value: "temporal-frontend:7233"
```

## Production Considerations

### Scaling

**API Servers:**
- Horizontal scaling based on HTTP traffic
- Use load balancer
- Stateless design

**Workers:**
- Scale based on workflow/activity load
- Monitor task queue depth
- Adjust concurrency settings

### Monitoring

**Metrics to track:**
- Workflow execution time
- Activity retry rates
- Task queue depth
- API response times
- Error rates

**Tools:**
- Prometheus for metrics
- Grafana for dashboards
- Temporal Web UI for workflow monitoring

### High Availability

**Temporal:**
- Run multiple frontend services
- Use managed Temporal Cloud
- Database replication

**Application:**
- Multiple API replicas
- Multiple worker replicas
- Health checks and auto-restart

### Security

**Best Practices:**
- Use TLS for Temporal connection
- Encrypt sensitive data
- Use secrets management (Vault, AWS Secrets Manager)
- Implement authentication/authorization
- Network policies in Kubernetes

### Database

**Production setup:**
- Use managed database (RDS, Cloud SQL)
- Enable backups
- Connection pooling
- Read replicas for queries

## Environment Variables

```bash
# Server
SERVER_PORT=8080
SERVER_HOST=0.0.0.0

# Temporal
TEMPORAL_HOST_PORT=temporal:7233
TEMPORAL_NAMESPACE=production
TEMPORAL_TASK_QUEUE=order-fulfillment

# Database
DB_HOST=postgres.example.com
DB_PORT=5432
DB_DATABASE=orders
DB_USERNAME=orderapp
DB_PASSWORD=<from-secrets>

# Logging
LOG_LEVEL=info
LOG_ENVIRONMENT=production
```

## Troubleshooting

**Worker not picking up tasks:**
- Check task queue name matches
- Verify Temporal connection
- Check worker logs

**Workflow failures:**
- Check Temporal UI for error details
- Review activity retry policies
- Check external service availability

**API errors:**
- Verify Temporal connection
- Check workflow registration
- Review API logs

## Rollback Strategy

1. Keep previous Docker images tagged
2. Use Kubernetes rolling updates
3. Monitor error rates during deployment
4. Quick rollback command ready:
```bash
kubectl rollout undo deployment/order-api
kubectl rollout undo deployment/order-worker
```
