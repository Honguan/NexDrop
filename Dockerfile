FROM node:24-alpine3.24 AS web-build
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY web/index.html web/tsconfig.json web/vite.config.ts ./
COPY web/src ./src
RUN npm run build

FROM golang:1.26.5-alpine3.24 AS build
ARG VERSION=2.0.1
ARG COMMIT=development
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X nexdrop/internal/version.ProductVersion=${VERSION} -X nexdrop/internal/version.BuildCommit=${COMMIT}" -o /out/nexdrop ./cmd/nexdrop

FROM alpine:3.24.1
ARG VERSION=2.0.1
ARG COMMIT=development
LABEL org.opencontainers.image.title="NexDrop Node" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/Honguan/NexDrop"
RUN apk add --no-cache postgresql17-client
RUN addgroup -S nexdrop && adduser -S -G nexdrop nexdrop
RUN mkdir -p /var/lib/nexdrop && chown nexdrop:nexdrop /var/lib/nexdrop
COPY --from=build /out/nexdrop /usr/local/bin/nexdrop
COPY --from=web-build /web/dist /usr/share/nexdrop/web
COPY migrations /usr/share/nexdrop/migrations
USER nexdrop
EXPOSE 8080
ENTRYPOINT ["nexdrop"]
