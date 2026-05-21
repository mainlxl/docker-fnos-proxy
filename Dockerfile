FROM scratch

WORKDIR /app
COPY docker-proxy /app/docker-proxy

EXPOSE 5001

ENTRYPOINT ["/app/docker-proxy"]
CMD ["-addr", ":5001", "-docker-config", "/app/config.json"]
