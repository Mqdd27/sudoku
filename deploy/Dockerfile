# Optional. The intended setup is the plain binary + systemd (see README).
# Build from the repo root: docker build -f deploy/Dockerfile -t sudoku .
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY shared /src/shared
COPY web ./
RUN npm run build

FROM golang:1.27-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist server/webdist
RUN CGO_ENABLED=0 go build -tags embedweb -trimpath -ldflags "-s -w" -o /sudoku-server ./server

FROM scratch
COPY --from=go /sudoku-server /sudoku-server
ENV DB_PATH=/data/sudoku.db
VOLUME /data
# Note: binds 127.0.0.1 unless BIND_ADDR is set (fly.toml sets 0.0.0.0); without it run with --network host.
ENTRYPOINT ["/sudoku-server", "-web"]
