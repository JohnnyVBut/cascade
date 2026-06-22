#!/bin/bash
set -e

cd "$(dirname "$0")/.."

echo "Pulling latest code..."
git pull origin master

# Detect whether this is a local build or GHCR install.
# Local build: cascade:latest image exists (created by ./build.sh).
if docker images cascade:latest --format '{{.ID}}' | grep -q .; then
    echo "Local build detected — rebuilding image..."
    ./build.sh
    docker tag cascade:latest ghcr.io/johnnyvbut/cascade:latest
else
    echo "Pulling latest Docker image from registry..."
    docker compose -f docker-compose.yml pull
fi

echo "Restarting container..."
docker compose -f docker-compose.yml down
docker compose -f docker-compose.yml up -d

echo "Done. Check logs: docker logs cascade"
