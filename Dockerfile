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

# TODO Confirm that SIGTERM is indeed the correct, safe signal. Also, how long should the grace period be?
# TODO Document: Build the zrepl Docker container: export ZREPL_VERSION=0.3.0 && sudo docker build --tag zrepl:${ZREPL_VERSION} --build-arg ZREPL_VERSION=${ZREPL_VERSION} .
# TODO Document: Running the zrepl Docker container: sudo docker run -d --name zrepl -v [ZREPL_CONF_DIR]:/etc/zrepl:ro --device /dev/zfs zrepl:0.3.0
# TODO Document: Provide your zrepl configuration as a volume to this running Docker container.
# TODO Document: Give the container access to ZFS.
# TODO Document: Execute zrepl subcommands using this pattern: docker exec -it zrepl zrepl [subcommand]
