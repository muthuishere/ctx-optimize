FROM golang:1.24 as builder
WORKDIR /src
COPY . .
RUN go build -o /out/api ./cmd/api

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/api /api
COPY --from=ghcr.io/golden/tools:3 /usr/bin/tool /usr/bin/tool
EXPOSE 8080
ENTRYPOINT ["/api"]
