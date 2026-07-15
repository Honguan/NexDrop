FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/nexdrop ./cmd/nexdrop

FROM alpine:3.21
RUN addgroup -S nexdrop && adduser -S -G nexdrop nexdrop
COPY --from=build /out/nexdrop /usr/local/bin/nexdrop
USER nexdrop
EXPOSE 8080
ENTRYPOINT ["nexdrop"]

