FROM alpine:3.20 AS certs
RUN apk --no-cache add ca-certificates

FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /app
COPY docker-fnos-proxy /app/docker-fnos-proxy

EXPOSE 5001

ENTRYPOINT ["/app/docker-fnos-proxy"]
CMD ["-addr", ":5001", "-docker-config", "/app/config.json"]
