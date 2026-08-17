FROM node:20-alpine

WORKDIR /app

COPY package.json .
COPY server.js .
COPY public/ public/

EXPOSE 3000

ENV PORT=3000
ENV API_BASE_URL=http://localhost:8080

CMD ["node", "server.js"]
