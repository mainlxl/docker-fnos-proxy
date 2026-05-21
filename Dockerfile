FROM scratch

COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
WORKDIR /app
COPY docker-fnos-proxy /app/docker-fnos-proxy

EXPOSE 5001

ENTRYPOINT ["/app/docker-fnos-proxy"]
CMD ["-addr", ":5001", "-docker-config", "/app/config.json"]
