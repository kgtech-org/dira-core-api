# Image de PRODUCTION de dira-core-api — LE SOCLE de la plateforme.
#
# Un seul binaire : `api`. Pas de worker — le socle ne fait rien en différé ;
# pas de seed — le provisionnement d'un jeu de démo appartient à la verticale
# qui sait ce qu'elle démontre, et il ouvre ses comptes par la surface de
# service.
#
# Pas de ffmpeg non plus : le socle ne traite ni vidéo ni vignette. L'image de
# la livraison le porte parce que son worker compresse des shorts ; l'installer
# ici pèserait cent mégaoctets pour rien.

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app

COPY --from=build /out/api /app/api
# LOCALES_PATH=locales (chemin relatif au WORKDIR) — voir docker-compose.
# Les gabarits de notification sont traduits ici : c'est le socle qui envoie,
# pour les deux verticales.
COPY locales ./locales

# 8082 : port distinct de la livraison (8080) et des courses (8081). Les trois
# cohabitent sur le même réseau Docker, et la passerelle route par préfixe.
ENV PORT=8082 \
    LOCALES_PATH=locales
EXPOSE 8082
USER app

ENTRYPOINT ["/app/api"]
