FROM golang:alpine AS builder

RUN apk add --no-cache build-base

WORKDIR /src

# Layer cache: scarica dipendenze prima del codice
COPY go.mod go.sum ./
RUN go mod download

# Copia sorgenti
COPY . .

ARG CGO_ENABLED=1
ARG GOOS=linux
ARG GOARCH=amd64
ARG CGO_CFLAGS="-D_LARGEFILE64_SOURCE"

RUN go build -trimpath -ldflags="-s -w" -o /out/main ./main


FROM alpine:latest AS deploy

RUN apk --no-cache add tzdata ghostscript ca-certificates su-exec && \
    adduser -D -u 10001 app && \
    mkdir -p /pb/main /pb_public && \
    chown -R app:app /pb /pb_public

COPY --from=builder --chown=app:app /out/main /pb/main/main
COPY --from=builder --chown=app:app /src/pb_public/ /pb_public/
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

WORKDIR /pb/main

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/pb/main/main", "serve", "--http=0.0.0.0:8080"]
