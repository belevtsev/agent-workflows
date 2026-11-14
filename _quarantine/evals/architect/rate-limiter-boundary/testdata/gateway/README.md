# gateway

Edge service for tenant APIs. Deployed as three replicas (`replicas: 3` in
`deploy/gateway.yaml`) behind the shared load balancer. Sessions live in the
shared Redis (`REDIS_ADDR`). Configuration is loaded once at start from
`config.yaml`.
