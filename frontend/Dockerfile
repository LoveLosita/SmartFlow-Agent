# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:22-bookworm
ARG NGINX_IMAGE=nginx:1.27-alpine

FROM ${NODE_IMAGE} AS builder

WORKDIR /app

ARG VITE_APP_BASE=/
ARG VITE_API_BASE_URL=/api/v1
ENV VITE_APP_BASE=${VITE_APP_BASE}
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM ${NGINX_IMAGE} AS runtime

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80
