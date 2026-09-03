# STORIA: questo builder e` stato pinnato a lungo a 1.25.9. Il bump a 1.25.10
# (CVE GO-2026-4918, patch sull'handling di HTTP/2 SETTINGS_MAX_FRAME_SIZE)
# cambiava il fingerprint h2 di Go quel tanto che basta perche` il Bot Manager
# di Azure Front Door davanti a cloud32.it ci classificasse come automation: la
# POST di login rispondeva 302 ma senza i cookie applicativi (id, codper,
# codass, date, logged), e il sync soci si fermava senza errori visibili.
#
# Risolto alla radice in area32.newHTTP1Transport: lo scraper annuncia solo
# http/1.1 in ALPN, quindi non manda piu` nessun frame SETTINGS e non c'e` piu`
# nessuna impronta h2 che una patch di Go possa spostare.
#
# Verificato in produzione con 1.25.9 + mitigazione: POST /api/cs/auth-with-zitadel
# ha riscritto il record del socio, e quella scrittura passa da
# upsertUserFromAreaUser, cioe` dallo scraping riuscito dall'egress reale.
# Solo dopo quella prova il pin e` stato liftato a 1.26.5, e poi a 1.26.6, che
# chiude le CVE stdlib rimaste (encoding/xml, encoding/asn1, net/http via
# x/net/idna) e fa passare govulncheck (quindi Security -> Build -> GHCR).
#
# Se il sync soci dovesse degradare di nuovo dopo un bump, l'ordine di indagine
# e` questo:
#   1. controllare che sia ancora HTTP/1.1 e che i cookie sopravvivano:
#        AREA32_PROBE=1 AREA32_PROBE_EMAIL=... AREA32_PROBE_PASSWORD=... \
#          go test ./area32/ -run TestProbeCloud32 -v
#      da dentro il server: dalla rete di uno sviluppatore passa in ogni caso,
#      perche` il Bot Manager pesa anche IP e reputazione.
#   2. se h2 e` gia` escluso, il sospetto resta il fingerprint TLS del
#      ClientHello, che cambia comunque fra versioni di Go. La strada e` utls,
#      per imitare il ClientHello di un browser.
FROM golang:1.26.6-alpine AS builder

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


# Build whisper.cpp (CPU only, AVX2 optimized) come fallback per la
# trascrizione podcast quando YouTube non espone i sottotitoli.
FROM alpine:latest AS whisper-builder
RUN apk --no-cache add build-base cmake git
WORKDIR /tmp
RUN git clone --depth 1 https://github.com/ggerganov/whisper.cpp /tmp/whisper.cpp
WORKDIR /tmp/whisper.cpp
RUN cmake -B build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
    && cmake --build build --config Release -j $(nproc) \
    && strip build/bin/whisper-cli


FROM alpine:latest AS deploy

RUN apk --no-cache add tzdata ghostscript ca-certificates su-exec ffmpeg yt-dlp libstdc++ libgomp && \
    adduser -D -u 10001 app && \
    mkdir -p /pb/main /pb_public && \
    chown -R app:app /pb /pb_public

COPY --from=builder --chown=app:app /out/main /pb/main/main
COPY --from=builder --chown=app:app /src/pb_public/ /pb_public/
# whisper.cpp binary per il fallback STT podcast (lazy model download
# nel volume pb_data al primo uso).
COPY --from=whisper-builder /tmp/whisper.cpp/build/bin/whisper-cli /usr/local/bin/whisper-cli
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

WORKDIR /pb/main

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/pb/main/main", "serve", "--http=0.0.0.0:8080"]
