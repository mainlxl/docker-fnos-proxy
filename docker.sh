#!/bin/bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o docker-proxy .
docker build -t docker-proxy:latest .

docker stop docker-proxy
docker rm docker-proxy

docker run -d -p 5001:5001 \
  --name docker-proxy \
  --restart always \
  --log-opt max-size=5m \
  --log-opt max-file=1 \
  -v /root/.docker/config.json:/app/config.json:ro \
  docker-proxy:latest
