# Build from the repo root: docker build -f services/api-gateway/Dockerfile .
FROM node:20-alpine AS build
WORKDIR /app
COPY services/api-gateway/package*.json ./
RUN npm ci
COPY services/api-gateway/tsconfig.json ./
COPY services/api-gateway/src ./src
RUN npm run build

FROM node:20-alpine
ENV NODE_ENV=production
WORKDIR /app
COPY services/api-gateway/package*.json ./
RUN npm ci --omit=dev
COPY --from=build /app/dist ./dist
# The ledger proto is the single source of truth; the gateway loads it at runtime.
COPY services/ledger/proto/ledger.proto ./proto/ledger.proto
ENV LEDGER_PROTO_PATH=/app/proto/ledger.proto
USER node
EXPOSE 3000
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:3000/health || exit 1
CMD ["node", "dist/index.js"]