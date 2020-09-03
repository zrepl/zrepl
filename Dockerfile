FROM debian:buster-slim

ARG ZREPL_VERSION
ENV ZREPL_VERSION=${ZREPL_VERSION}

RUN set -eux && \
    # Add APT repositories
    apt-get update && apt-get install --yes --no-install-recommends ca-certificates curl gnupg lsb-release && \
    (curl -fsSL --insecure https://zrepl.cschwarz.com/apt/apt-key.asc | apt-key add -) && \
    ( \
      . /etc/os-release && \
      ARCH="$(dpkg --print-architecture)" && \
      echo "deb [arch=$ARCH] https://zrepl.cschwarz.com/apt/$ID $VERSION_CODENAME main" > /etc/apt/sources.list.d/zrepl.list && \
      echo "deb http://deb.debian.org/$ID stable contrib" > /etc/apt/sources.list.d/stable-contrib.list \
    ) && \
    apt-get update && \
    # Install zrepl and its user-land ZFS utils dependency
    apt-get install --yes --no-install-recommends zrepl zfsutils-linux && \
    # zrepl expects /var/run/zrepl
    mkdir -p /var/run/zrepl && chmod 0700 /var/run/zrepl && \
    # Reduce final Docker image size: Clear the APT cache
    apt-get clean && rm -rf /var/lib/apt/lists/* # Remove unused packages and package cache

CMD ["daemon"]
ENTRYPOINT ["/usr/bin/zrepl", "--config", "/etc/zrepl/zrepl.yml"]
STOPSIGNAL SIGTERM
VOLUME /etc/zrepl
WORKDIR "/etc/zrepl"
