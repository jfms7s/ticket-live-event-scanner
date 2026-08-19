FROM node:20-alpine AS build

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY tsconfig*.json vite.config.ts index.html ./
COPY src/ src/
RUN npm run build

FROM node:20-alpine

WORKDIR /app

COPY package.json .
COPY server.js .
COPY --from=build /app/dist dist/

RUN chown -R 1000:1000 /app

USER 1000:1000

EXPOSE 3000

ENV PORT=3000
ENV API_BASE_URL=http://localhost:8080

CMD ["node", "server.js"]
