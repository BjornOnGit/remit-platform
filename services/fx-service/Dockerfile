# Build from the repo root: docker build -f services/fx-service/Dockerfile .
FROM node:20-alpine AS build
WORKDIR /app
COPY services/fx-service/package*.json ./
RUN npm ci
COPY services/fx-service/tsconfig.json ./
COPY services/fx-service/src ./src
RUN npm run build

FROM node:20-alpine
ENV NODE_ENV=production
WORKDIR /app
COPY services/fx-service/package*.json ./
RUN npm ci --omit=dev
COPY --from=build /app/dist ./dist
COPY services/fx-service/data ./data
USER node
EXPOSE 3001
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:3001/health || exit 1
CMD ["node", "dist/index.js"]