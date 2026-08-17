FROM node:20-alpine

WORKDIR /app

COPY package.json .
COPY server.js .
COPY public/ public/

RUN mkdir -p /app/public && chown -R 1000:1000 /app

USER 1000:1000

EXPOSE 3000

ENV PORT=3000
ENV API_BASE_URL=http://localhost:8080

CMD ["node", "server.js"]
