# syntax=docker/dockerfile:1.7

# ---- builder: install deps and produce the production Nitro server ----
FROM node:22-bookworm AS builder
WORKDIR /app/frontend

RUN corepack enable

COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# VITE_API_HOST is baked into the client bundle at build time. Override via
# `--build-arg VITE_API_HOST=...` (compose passes it through build.args).
ARG VITE_API_HOST=localhost:38080
ENV VITE_API_HOST=${VITE_API_HOST}

COPY . ./
RUN pnpm build

# ---- runner: ship only the self-contained Nitro output ----
FROM node:22-bookworm-slim AS runner
WORKDIR /app/frontend

ENV NODE_ENV=production
# Nitro's node-server honours PORT/HOST.
ENV PORT=3000
ENV HOST=0.0.0.0

COPY --from=builder /app/frontend/.output ./.output

EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
