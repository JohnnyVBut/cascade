#!/bin/bash
set -e

cd "$(dirname "$0")/.."

echo "Pulling latest code..."
git pull origin master

echo "Pulling latest Docker image..."
docker compose -f docker-compose.yml pull

echo "Restarting container..."
docker compose -f docker-compose.yml up -d

echo "Done. Check logs: docker logs cascade"
