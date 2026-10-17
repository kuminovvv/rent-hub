FROM golang:1.26-alpine AS build-base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM build-base AS build-api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/api

FROM build-base AS build-telegram-sync
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/telegram-sync

FROM build-base AS build-catalog-rebuild
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/catalog-rebuild

FROM build-base AS build-catalog-geocode
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/catalog-geocode

FROM build-base AS build-catalog-buildings
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/catalog-buildings

FROM alpine:3.21 AS runtime-base
RUN addgroup -S app && adduser -S -G app app
USER app

FROM runtime-base AS api
COPY --from=build-api /out/app /usr/local/bin/api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]

FROM runtime-base AS telegram-sync
COPY --from=build-telegram-sync /out/app /usr/local/bin/telegram-sync
ENTRYPOINT ["/usr/local/bin/telegram-sync"]

FROM runtime-base AS catalog-rebuild
COPY --from=build-catalog-rebuild /out/app /usr/local/bin/catalog-rebuild
ENTRYPOINT ["/usr/local/bin/catalog-rebuild"]

FROM runtime-base AS catalog-geocode
COPY --from=build-catalog-geocode /out/app /usr/local/bin/catalog-geocode
ENTRYPOINT ["/usr/local/bin/catalog-geocode"]

FROM runtime-base AS catalog-buildings
COPY --from=build-catalog-buildings /out/app /usr/local/bin/catalog-buildings
ENTRYPOINT ["/usr/local/bin/catalog-buildings"]
