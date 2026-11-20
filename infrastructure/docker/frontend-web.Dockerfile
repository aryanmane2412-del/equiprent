# syntax=docker/dockerfile:1.7
#
# Builds the Flutter web app and serves it with nginx (SPA fallback).
#   docker build -f infrastructure/docker/frontend-web.Dockerfile \
#     --build-arg API_BASE_URL=http://localhost:8080 frontend/equiprent_app
#
# Note: the Flutter build image is several GB. On machines with little disk
# space, prefer `scripts/run-app.ps1` for local web development.

ARG FLUTTER_VERSION=3.44.7

FROM ghcr.io/cirruslabs/flutter:${FLUTTER_VERSION} AS build
WORKDIR /app
COPY pubspec.yaml pubspec.lock ./
RUN flutter pub get
COPY . .
ARG API_BASE_URL
ARG APP_ENV=development
RUN test -n "$API_BASE_URL" || (echo "API_BASE_URL build arg is required" && exit 1)
RUN flutter build web --release --no-web-resources-cdn \
      --dart-define=API_BASE_URL=${API_BASE_URL} \
      --dart-define=APP_ENV=${APP_ENV}

FROM nginx:1.27-alpine
COPY <<'EOF' /etc/nginx/conf.d/default.conf
server {
  listen 80;
  root /usr/share/nginx/html;

  add_header X-Content-Type-Options nosniff always;
  add_header Referrer-Policy strict-origin-when-cross-origin always;
  add_header X-Frame-Options DENY always;

  # Never cache the entry points, so new deployments are picked up.
  location = /index.html            { add_header Cache-Control "no-cache"; }
  location = /flutter_bootstrap.js  { add_header Cache-Control "no-cache"; }
  location = /flutter_service_worker.js { add_header Cache-Control "no-cache"; }

  # Deep links (/owner/equipment) fall back to the SPA.
  location / { try_files $uri $uri/ /index.html; }
}
EOF
COPY --from=build /app/build/web /usr/share/nginx/html
EXPOSE 80
