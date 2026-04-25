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

RUN apk --no-cache add tzdata ghostscript ca-certificates && \
    adduser -D -u 10001 app && \
    mkdir -p /pb/main /pb_public && \
    chown -R app:app /pb /pb_public

COPY --from=builder --chown=app:app /out/main /pb/main/main
COPY --from=builder --chown=app:app /src/pb_public/ /pb_public/

USER app
WORKDIR /pb/main

EXPOSE 8080
CMD ["/pb/main/main", "serve", "--http=0.0.0.0:8080"]
