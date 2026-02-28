#!/bin/sh
set -e

git pull
docker compose pull
docker compose build agent
docker compose up -d
docker system prune -f
