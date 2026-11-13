FROM ghcr.io/cirruslabs/flutter:3.41.6 AS ui
WORKDIR /src
COPY clients/flutter/ ./
RUN flutter pub get && flutter build web --release --no-web-resources-cdn

FROM golang:1.27.3 AS core
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY cmd/agenthub-service/ ./cmd/agenthub-service/
RUN mkdir -p /data && chown 65532:65532 /data
RUN CGO_ENABLED=0 go build -trimpath -o /agenthub-service ./cmd/agenthub-service

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=core /agenthub-service /agenthub-service
COPY --from=core --chown=65532:65532 /data /data
COPY --from=ui /src/build/web /web
VOLUME /data
EXPOSE 26331/tcp 26330/tcp 26330/udp
ENTRYPOINT ["/agenthub-service"]
CMD ["--listen","0.0.0.0:26331","--state","/data/devices.json","--web","/web"]
