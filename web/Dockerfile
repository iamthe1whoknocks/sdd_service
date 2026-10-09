# VulnRAG SPA — сборка Vite, раздача через nginx.
# nginx проксирует /api/* и /health на Go-приложение (same-origin, CORS не нужен).

# ---- Stage 1: сборка SPA ----
FROM node:20-alpine AS build

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund

COPY . .
RUN npm run build

# ---- Stage 2: nginx ----
FROM nginx:1.27-alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80