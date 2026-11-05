# syntax=docker/dockerfile:1
FROM curlimages/curl:8.23.0@sha256:3fb7e13e5d179a845c97d78204f3f1064d1907037f26a3ebf91710571151fd93 AS runtime
LABEL org.opencontainers.image.source="https://github.com/belevtsev/agent-workflows"
WORKDIR /work
USER 10001:10001
ENTRYPOINT ["/fixtures/cwdev"]
