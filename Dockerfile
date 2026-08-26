# syntax=docker/dockerfile:1

# Budowa statyczna i powtarzalna: -trimpath usuwa sciezki budowy, -buildid=
# usuwa niedeterministyczny identyfikator, CGO_ENABLED=0 daje binarke bez
# zaleznosci systemowych. Dwie budowy z tego samego commitu daja ten sam
# artefakt — bez tego wymaganie R7 z SEAL.md (obraz weryfikowalny po digescie)
# nie mialoby sensu.
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w -buildid=" -o /out/beat-key . \
 && mkdir -p /data \
 && chown 65532:65532 /data

# distroless/static: brak powloki, brak menedzera pakietow, brak niczego poza
# binarka. Nie ma sie do czego zalogowac, wiec nie ma czym wyniesc klucza.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/beat-key /beat-key
# Katalog kopiowany razem z wlascicielem — nazwany wolumen dziedziczy te prawa
# przy inicjalizacji, inaczej uzytkownik nonroot nie zapisalby klucza.
COPY --from=build --chown=65532:65532 /data /data

USER nonroot:nonroot
EXPOSE 8080
VOLUME ["/data"]

# Domyslne BEAT_KEY_DIR=/data i BEAT_KEY_ADDR=:8080 siedza w kodzie.

ENTRYPOINT ["/beat-key"]
