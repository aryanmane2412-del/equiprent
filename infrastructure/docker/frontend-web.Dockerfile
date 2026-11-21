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
ARG API_BASE_URL
# nginx drops server-level add_header directives inside any location that sets
# its own add_header (M26), so every header lives in one snippet that each
# location includes. CSP: Flutter web (CanvasKit) needs wasm-unsafe-eval,
# inline bootstrap script/style and blob workers; the API origin is injected.
COPY <<'EOF' /etc/nginx/snippets-security.conf
add_header X-Content-Type-Options "nosniff" always;
add_header Referrer-Policy "strict-origin-when-cross-origin" always;
add_header X-Frame-Options "DENY" always;
add_header Permissions-Policy "geolocation=(self), camera=(self), microphone=(), payment=(self)" always;
add_header Content-Security-Policy "default-src 'self'; script-src 'self' 'wasm-unsafe-eval' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self' __API_ORIGIN__ __WS_ORIGIN__ https://nominatim.openstreetmap.org; worker-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'" always;
EOF
COPY <<'EOF' /etc/nginx/conf.d/default.conf
server {
  listen 80;
  root /usr/share/nginx/html;
  server_tokens off;
  include /etc/nginx/snippets-security.conf;

  # Never cache the entry points, so new deployments are picked up.
  location = /index.html            { include /etc/nginx/snippets-security.conf; add_header Cache-Control "no-cache" always; }
  location = /flutter_bootstrap.js  { include /etc/nginx/snippets-security.conf; add_header Cache-Control "no-cache" always; }
  location = /flutter_service_worker.js { include /etc/nginx/snippets-security.conf; add_header Cache-Control "no-cache" always; }

  # Deep links (/owner/equipment) fall back to the SPA.
  location / { include /etc/nginx/snippets-security.conf; try_files $uri $uri/ /index.html; }
}
EOF
RUN o="${API_BASE_URL%/}" && w="$(echo "$o" | sed 's|^http|ws|')"  && sed -i "s|__API_ORIGIN__|$o|; s|__WS_ORIGIN__|$w|" /etc/nginx/snippets-security.conf
COPY --from=build /app/build/web /usr/share/nginx/html
EXPOSE 80
