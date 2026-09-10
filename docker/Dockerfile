# Image de PRODUCTION de dira-core-api — LE SOCLE de la plateforme.
#
# Deux binaires : `api` et `seed`. Pas de worker — le socle ne fait rien en
# différé.
#
# ⚠️ `seed` n'amorce QUE l'administrateur. Le jeu de démonstration appartient à
# la verticale qui sait ce qu'elle démontre, et elle ouvre ses comptes par la
# surface de service. Ce binaire existe pour un cas précis : un socle déployé
# SEUL n'a personne pour s'y connecter.
#
# Pas de ffmpeg non plus : le socle ne traite ni vidéo ni vignette. L'image de
# la livraison le porte parce que son worker compresse des shorts ; l'installer
# ici pèserait cent mégaoctets pour rien.

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/seed ./cmd/seed

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app

COPY --from=build /out/api /app/api
# Provisionnement one-shot de l'administrateur : lancé explicitement, jamais au
# démarrage. ⚠️ `--reset` VIDE les comptes de TOUTE la plateforme.
COPY --from=build /out/seed /app/seed
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
