# The build output is architecture-independent, so build natively instead
# of under QEMU for arm64.
FROM --platform=$BUILDPLATFORM node:24-alpine AS build

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY tsconfig*.json vite.config.ts index.html ./
COPY src/ src/
RUN npm run build

FROM node:24-alpine

WORKDIR /app

COPY package.json .
COPY server.js server-app.js ./
COPY --from=build /app/dist dist/

# Files stay root-owned: the server writes nothing (config.js is served
# from memory), so its code and assets aren't writable by the runtime user.
USER 1000:1000

EXPOSE 3000

ENV PORT=3000
ENV API_BASE_URL=http://localhost:8080

CMD ["node", "server.js"]
