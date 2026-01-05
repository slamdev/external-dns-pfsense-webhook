# syntax=docker/dockerfile:1
FROM debian:bookworm-slim

ARG TARGETOS
ARG TARGETARCH

ENV HOME="/" \
    XDG_CACHE_HOME="/tmp" \
    OS_ARCH="${TARGETARCH}" \
    OS_FLAVOUR="debian-12" \
    OS_NAME="linux"

SHELL ["/bin/bash", "-o", "errexit", "-o", "nounset", "-o", "pipefail", "-c"]

RUN apt-get update -qq  \
 && apt-get upgrade -y \
 && apt-get install -y --no-install-recommends ca-certificates curl procps \
 && apt-get clean  \
 && rm -rf /var/lib/apt/lists /var/cache/apt/archives \
 && find / -perm /6000 -type f -exec chmod a-s {} \; || true \
 && groupadd --gid 1001 appuser \
 && useradd --uid 1001 --gid appuser --shell /bin/bash --create-home appuser

COPY --link "bin/app-${TARGETOS}-${TARGETARCH}" /opt/app
RUN chmod +x /opt/app && chown 1001:1001 /opt/app

USER 1001

ENTRYPOINT [ "/opt/app" ]
