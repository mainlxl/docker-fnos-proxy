FROM scratch

WORKDIR /app
COPY docker-proxy /app/docker-fnos-proxy

EXPOSE 5001

ENTRYPOINT ["/app/docker-fnos-proxy"]
CMD ["-addr", ":5001", "-docker-config", "/app/config.json"]
