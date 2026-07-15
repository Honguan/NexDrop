FROM node:24-alpine AS web-build
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY web/index.html web/tsconfig.json web/vite.config.ts ./
COPY web/src ./src
RUN npm run build

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/nexdrop ./cmd/nexdrop

FROM alpine:3.21
RUN apk add --no-cache postgresql17-client
RUN addgroup -S nexdrop && adduser -S -G nexdrop nexdrop
RUN mkdir -p /var/lib/nexdrop && chown nexdrop:nexdrop /var/lib/nexdrop
COPY --from=build /out/nexdrop /usr/local/bin/nexdrop
COPY --from=web-build /web/dist /usr/share/nexdrop/web
USER nexdrop
EXPOSE 8080
ENTRYPOINT ["nexdrop"]
