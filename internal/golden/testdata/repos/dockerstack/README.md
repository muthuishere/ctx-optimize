# dockerstack

Golden fixture for the Docker + Compose recognizers (ADR
2026-07-25-docker-compose-recognizer). The compose file and the k8s manifest
deliberately share the image `ghcr.io/golden/api:2.0.0`, so the snapshot pins
that BOTH lanes converge on ONE `image:` node.
