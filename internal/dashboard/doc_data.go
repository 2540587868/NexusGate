package dashboard

var configSchema = map[string]interface{}{
	"server": map[string]interface{}{
		"listen":           map[string]interface{}{"type": "string", "default": ":8080", "description": "HTTP listen address"},
		"tls_listen":       map[string]interface{}{"type": "string", "default": "", "description": "HTTPS listen address"},
		"tls_cert":         map[string]interface{}{"type": "string", "default": "", "description": "TLS certificate path"},
		"tls_key":          map[string]interface{}{"type": "string", "default": "", "description": "TLS private key path"},
		"metrics_listen":   map[string]interface{}{"type": "string", "default": ":9090", "description": "Prometheus metrics listen address"},
		"dashboard_listen": map[string]interface{}{"type": "string", "default": ":8081", "description": "Dashboard listen address"},
		"dashboard_token":  map[string]interface{}{"type": "string", "default": "", "description": "Dashboard authentication token"},
	},
	"gateway": map[string]interface{}{
		"shard_count":             map[string]interface{}{"type": "int", "default": "64", "description": "Number of shards for request distribution"},
		"worker_per_shard":        map[string]interface{}{"type": "int", "default": "1", "description": "Workers per shard"},
		"queue_size":              map[string]interface{}{"type": "int", "default": "1024", "description": "Queue size per shard"},
		"slow_recovery_threshold": map[string]interface{}{"type": "int", "default": "3", "description": "Consecutive failures before slow recovery"},
	},
	"proxy": map[string]interface{}{
		"connect_timeout":   map[string]interface{}{"type": "duration", "default": "5s", "description": "Backend connect timeout"},
		"read_timeout":      map[string]interface{}{"type": "duration", "default": "30s", "description": "Backend read timeout"},
		"write_timeout":     map[string]interface{}{"type": "duration", "default": "30s", "description": "Backend write timeout"},
		"max_response_body": map[string]interface{}{"type": "int64", "default": "10485760", "description": "Max response body bytes"},
	},
	"logging": map[string]interface{}{
		"level":  map[string]interface{}{"type": "string", "default": "info", "enum": []string{"debug", "info", "warn", "error"}, "description": "Log level"},
		"format": map[string]interface{}{"type": "string", "default": "json", "enum": []string{"json", "text"}, "description": "Log format"},
	},
	"middleware": map[string]interface{}{
		"order": map[string]interface{}{"type": "[]string", "default": "[trace,ratelimit,cors,circuitbreaker]", "description": "Middleware execution order"},
		"ratelimit": map[string]interface{}{
			"type": "object",
			"fields": map[string]interface{}{
				"requests_per_second": map[string]interface{}{"type": "float64", "default": "100", "description": "Requests per second per tenant"},
				"burst":               map[string]interface{}{"type": "int", "default": "200", "description": "Burst size"},
			},
		},
		"auth": map[string]interface{}{
			"type": "object",
			"fields": map[string]interface{}{
				"type":            map[string]interface{}{"type": "string", "enum": []string{"none", "api_key", "jwt"}, "description": "Auth type"},
				"jwt_hmac_secret": map[string]interface{}{"type": "string", "description": "HMAC secret for JWT validation"},
				"jwt_issuer":      map[string]interface{}{"type": "string", "description": "Expected JWT issuer"},
				"jwt_audience":    map[string]interface{}{"type": "string", "description": "Expected JWT audience"},
				"skip_paths":      map[string]interface{}{"type": "[]string", "description": "Paths to skip auth"},
			},
		},
		"cors": map[string]interface{}{
			"type": "object",
			"fields": map[string]interface{}{
				"allowed_origins":   map[string]interface{}{"type": "[]string", "description": "Allowed origins"},
				"allowed_methods":   map[string]interface{}{"type": "[]string", "description": "Allowed HTTP methods"},
				"allowed_headers":   map[string]interface{}{"type": "[]string", "description": "Allowed request headers"},
				"allow_credentials": map[string]interface{}{"type": "bool", "default": "false", "description": "Allow credentials"},
				"max_age":           map[string]interface{}{"type": "int", "default": "86400", "description": "Preflight cache max age (seconds)"},
			},
		},
		"circuitbreaker": map[string]interface{}{
			"type": "object",
			"fields": map[string]interface{}{
				"failure_threshold": map[string]interface{}{"type": "int", "default": "5", "description": "Failures before opening"},
				"success_threshold": map[string]interface{}{"type": "int", "default": "3", "description": "Successes before closing"},
				"timeout":           map[string]interface{}{"type": "duration", "default": "30s", "description": "Open state duration"},
			},
		},
		"tenant": map[string]interface{}{
			"type": "object",
			"fields": map[string]interface{}{
				"header_name":    map[string]interface{}{"type": "string", "default": "X-Tenant-ID", "description": "Header name for tenant identification"},
				"default_tenant": map[string]interface{}{"type": "string", "default": "", "description": "Default tenant ID when header is missing"},
				"tenants":        map[string]interface{}{"type": "[]TenantConfig", "description": "List of tenant configurations"},
			},
		},
	},
	"routes": map[string]interface{}{
		"type": "array",
		"item": map[string]interface{}{
			"match": map[string]interface{}{
				"type": "object",
				"fields": map[string]interface{}{
					"path_prefix": map[string]interface{}{"type": "string", "description": "Path prefix to match"},
					"path_exact":  map[string]interface{}{"type": "string", "description": "Exact path to match"},
					"path_regex":  map[string]interface{}{"type": "string", "description": "Regex pattern to match"},
					"methods":     map[string]interface{}{"type": "[]string", "description": "HTTP methods to match"},
					"headers":     map[string]interface{}{"type": "map[string]string", "description": "Headers to match"},
				},
			},
			"backend":   map[string]interface{}{"type": "[]BackendConfig", "description": "Backend servers"},
			"strategy":  map[string]interface{}{"type": "string", "enum": []string{"round_robin", "weighted_round_robin", "random", "consistent_hash", "ip_hash", "canary"}, "description": "Load balancing strategy"},
			"middleware": map[string]interface{}{"type": "[]string", "description": "Per-route middleware chain"},
			"canary":    map[string]interface{}{"type": "CanaryRule", "description": "Canary deployment rule"},
			"timeout": map[string]interface{}{
				"type": "object",
				"fields": map[string]interface{}{
					"connect": map[string]interface{}{"type": "duration", "description": "Per-route connect timeout"},
					"read":    map[string]interface{}{"type": "duration", "description": "Per-route read timeout"},
					"write":   map[string]interface{}{"type": "duration", "description": "Per-route write timeout"},
					"total":   map[string]interface{}{"type": "duration", "description": "Per-route total timeout"},
				},
			},
			"retry": map[string]interface{}{
				"type": "object",
				"fields": map[string]interface{}{
					"max_retries":      map[string]interface{}{"type": "int", "description": "Max retry attempts"},
					"retryable_status": map[string]interface{}{"type": "[]int", "description": "HTTP status codes that trigger retry"},
				},
			},
			"streaming": map[string]interface{}{"type": "bool", "default": "false", "description": "Enable streaming response forwarding"},
			"rewrite": map[string]interface{}{
				"type": "object",
				"fields": map[string]interface{}{
					"request_header":  map[string]interface{}{"type": "HeaderRewrite", "description": "Request header rewrite rules"},
					"response_header": map[string]interface{}{"type": "HeaderRewrite", "description": "Response header rewrite rules"},
					"request_body":    map[string]interface{}{"type": "[]RewriteRule", "description": "Request body regex rewrite rules"},
					"response_body":   map[string]interface{}{"type": "[]RewriteRule", "description": "Response body regex rewrite rules"},
				},
			},
		},
	},
	"env_prefix": "NEXUSGATE_",
	"env_overrides": []string{
		"NEXUSGATE_SERVER_LISTEN",
		"NEXUSGATE_SERVER_TLS_LISTEN",
		"NEXUSGATE_SERVER_TLS_CERT",
		"NEXUSGATE_SERVER_TLS_KEY",
		"NEXUSGATE_SERVER_METRICS_LISTEN",
		"NEXUSGATE_SERVER_DASHBOARD_LISTEN",
		"NEXUSGATE_SERVER_DASHBOARD_TOKEN",
		"NEXUSGATE_LOGGING_LEVEL",
		"NEXUSGATE_LOGGING_FORMAT",
		"NEXUSGATE_GATEWAY_SHARD_COUNT",
		"NEXUSGATE_GATEWAY_WORKER_PER_SHARD",
		"NEXUSGATE_GATEWAY_QUEUE_SIZE",
		"NEXUSGATE_PROXY_CONNECT_TIMEOUT",
		"NEXUSGATE_PROXY_READ_TIMEOUT",
		"NEXUSGATE_PROXY_WRITE_TIMEOUT",
		"NEXUSGATE_RATELIMIT_RPS",
		"NEXUSGATE_RATELIMIT_BURST",
		"NEXUSGATE_AUTH_TYPE",
		"NEXUSGATE_AUTH_JWT_SECRET",
		"NEXUSGATE_CONFIG_ALLOW_PRIVATE_BACKENDS",
	},
}

func buildOpenAPIDoc(version string) map[string]interface{} {
	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "NexusGate API",
			"description": "NexusGate API Gateway Dashboard API",
			"version":     version,
		},
		"paths": map[string]interface{}{
			"/api/v1/overview": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Get gateway overview",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Overview data"},
					},
				},
			},
			"/api/v1/routes": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "List all routes",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Route list"},
					},
				},
				"post": map[string]interface{}{
					"summary": "Create a new route",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{"$ref": "#/components/schemas/RouteConfig"},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Route created"},
						"400": map[string]interface{}{"description": "Invalid request"},
					},
				},
			},
			"/api/v1/routes/{id}": map[string]interface{}{
				"get":    map[string]interface{}{"summary": "Get route by ID"},
				"put":    map[string]interface{}{"summary": "Update route"},
				"delete": map[string]interface{}{"summary": "Delete route"},
			},
			"/api/v1/routes/{id}/backends": map[string]interface{}{
				"post": map[string]interface{}{"summary": "Add backend to route"},
			},
			"/api/v1/routes/{id}/backends/{address}": map[string]interface{}{
				"delete": map[string]interface{}{"summary": "Remove backend from route"},
			},
			"/api/v1/backends": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "List all backends with health status",
				},
			},
			"/api/v1/config": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Get current configuration (secrets masked)",
				},
			},
			"/api/v1/topology": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Get traffic topology graph",
				},
			},
			"/api/v1/gateway": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Get gateway runtime stats",
				},
			},
			"/api/v1/auth": map[string]interface{}{
				"post": map[string]interface{}{
					"summary": "Authenticate and get session cookie",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"token": map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
				},
			},
			"/api/v1/circuitbreaker/reset": map[string]interface{}{
				"post": map[string]interface{}{"summary": "Reset circuit breaker state"},
			},
			"/api/v1/ratelimit": map[string]interface{}{
				"put": map[string]interface{}{
					"summary": "Update rate limit configuration",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"requests_per_second": map[string]interface{}{"type": "number"},
										"burst":               map[string]interface{}{"type": "integer"},
									},
								},
							},
						},
					},
				},
			},
			"/api/v1/tenants": map[string]interface{}{
				"get":  map[string]interface{}{"summary": "List all tenants"},
				"post": map[string]interface{}{"summary": "Create a new tenant"},
			},
			"/api/v1/tenants/{id}": map[string]interface{}{
				"put":    map[string]interface{}{"summary": "Update tenant configuration"},
				"delete": map[string]interface{}{"summary": "Delete tenant"},
			},
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"RouteConfig": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"match":    map[string]interface{}{"type": "object"},
						"backend":  map[string]interface{}{"type": "array"},
						"strategy": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
}
