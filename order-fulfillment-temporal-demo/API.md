# API Documentation

## Base URL
```
http://localhost:8080/api/v1
```

## Endpoints

### 1. Create Order
Start a new order fulfillment workflow.

**Endpoint:** `POST /orders`

**Request Body:**
```json
{
  "customer_id": "cust-123",
  "items": [
    {
      "product_id": "prod-456",
      "quantity": 2,
      "price": 29.99
    }
  ]
}
```

**Response:**
```json
{
  "order_id": "order-789",
  "workflow_id": "order-789",
  "run_id": "abc123...",
  "status": "pending"
}
```

### 2. Get Order
Retrieve order details by querying workflow state.

**Endpoint:** `GET /orders/:id`

**Response:**
```json
{
  "order_id": "order-789",
  "status": "processing",
  "inventory_held": true,
  "payment_id": "pay-123",
  "shipment_id": "ship-456",
  "last_updated": "2024-01-15T10:30:00Z"
}
```

### 3. List Orders
List all orders with optional filters.

**Endpoint:** `GET /orders?status=pending&customer_id=cust-123`

**Response:**
```json
{
  "orders": [
    {
      "order_id": "order-789",
      "customer_id": "cust-123",
      "status": "pending",
      "total_amount": 59.98,
      "created_at": "2024-01-15T10:00:00Z"
    }
  ],
  "total": 1
}
```

### 4. Cancel Order
Send cancel signal to running workflow.

**Endpoint:** `POST /orders/:id/cancel`

**Request Body:**
```json
{
  "reason": "Customer requested cancellation",
  "requested_by": "customer"
}
```

**Response:**
```json
{
  "success": true,
  "message": "Cancellation signal sent"
}
```

### 5. Update Order
Send update signal to workflow.

**Endpoint:** `PATCH /orders/:id`

**Request Body:**
```json
{
  "field": "shipping_address",
  "value": {
    "street": "123 Main St",
    "city": "New York",
    "state": "NY",
    "postal_code": "10001"
  }
}
```

**Response:**
```json
{
  "success": true,
  "message": "Update signal sent"
}
```

### 6. Get Order Status
Query current order status.

**Endpoint:** `GET /orders/:id/status`

**Response:**
```json
{
  "order_id": "order-789",
  "status": "shipped",
  "last_updated": "2024-01-15T12:00:00Z"
}
```

### 7. Get Order Progress
Query workflow execution progress.

**Endpoint:** `GET /orders/:id/progress`

**Response:**
```json
{
  "total_steps": 5,
  "completed_steps": 3,
  "current_step": "processing_shipment",
  "percent_done": 60.0,
  "estimated_time": 300
}
```

### 8. Health Check
Check API health status.

**Endpoint:** `GET /health`

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:00:00Z"
}
```

## Order Status Values

- `pending` - Order created, workflow started
- `processing` - Workflow executing
- `paid` - Payment successful
- `shipped` - Shipment created
- `delivered` - Order delivered
- `cancelled` - Order cancelled
- `failed` - Workflow failed

## Error Responses

**400 Bad Request:**
```json
{
  "error": "Invalid request",
  "message": "Missing required field: customer_id"
}
```

**404 Not Found:**
```json
{
  "error": "Order not found",
  "message": "Order with ID order-789 does not exist"
}
```

**500 Internal Server Error:**
```json
{
  "error": "Internal error",
  "message": "Failed to start workflow"
}
```

## Workflow Interaction

The API interacts with Temporal workflows:

1. **POST /orders** → Starts OrderWorkflow
2. **POST /orders/:id/cancel** → Sends cancel signal
3. **PATCH /orders/:id** → Sends update signal
4. **GET /orders/:id** → Queries workflow state
5. **GET /orders/:id/status** → Queries status
6. **GET /orders/:id/progress** → Queries progress

## Testing with cURL

```bash
# Create order
curl -X POST http://localhost:8080/api/v1/orders \
  -H "Content-Type: application/json" \
  -d '{"customer_id":"cust-123","items":[{"product_id":"prod-456","quantity":2,"price":29.99}]}'

# Get order
curl http://localhost:8080/api/v1/orders/order-789

# Cancel order
curl -X POST http://localhost:8080/api/v1/orders/order-789/cancel \
  -H "Content-Type: application/json" \
  -d '{"reason":"Customer request"}'
```
