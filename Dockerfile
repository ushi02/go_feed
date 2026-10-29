# Build context is the repo root so one image holds both the API and the built frontend.
FROM node:22-alpine AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

FROM golang:1.26 AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN CGO_ENABLED=0 go build -o /api ./cmd/api && CGO_ENABLED=0 go build -o /worker ./cmd/worker

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /api /api
COPY --from=build /worker /worker
COPY --from=web /web/dist /web
ENV STATIC_DIR=/web
EXPOSE 8080
CMD ["/api"]
