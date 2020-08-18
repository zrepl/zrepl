FROM debian:stable-slim

# TODO Document: Build the zrepl Docker container: export ZREPL_VERSION=0.2.1 sudo docker build --tag zrepl:${ZREPL_VERSION} --build-arg ZREPL_VERSION=${ZREPL_VERSION} .
# TODO Document: Running the zrepl Docker container: sudo docker run -d --name zrepl -v [ZREPL_CONF_DIR]:/etc/zrepl:ro --device /dev/zfs zrepl:0.2.1
# TODO Document: Provide your zrepl configuration as a volume to this running Docker container.
# TODO Document: Give the container access to ZFS.
# TODO Document: Execute zrepl subcommands using this pattern: docker exec -it zrepl zrepl [subcommand]

ARG ZREPL_VERSION
ENV ZREPL_VERSION=${ZREPL_VERSION}

RUN set -eux && \
    # Add the zrepl APT repository
    apt-get update && apt-get install --yes curl gnupg lsb-release && \
    (curl -fsSL https://zrepl.cschwarz.com/apt/apt-key.asc | apt-key add -) && \
    ( \
      . /etc/os-release && \
      ARCH="$(dpkg --print-architecture)" && \
      echo "deb [arch=$ARCH] https://zrepl.cschwarz.com/apt/$ID $VERSION_CODENAME main" > /etc/apt/sources.list.d/zrepl.list && \
      echo "deb http://deb.debian.org/$ID stable contrib" > /etc/apt/sources.list.d/stable-contrib.list \
    ) && \
    apt-get update && \
    # Install zrepl and dependencies
    apt-get --yes install zrepl=${ZREPL_VERSION} zfsutils-linux && \
    # Reduce final Docker image size: Clear the APT cache
    apt-get clean

CMD ["--config", "/etc/zrepl/zrepl.yml", "daemon"]
ENTRYPOINT ["/usr/bin/zrepl"]
STOPSIGNAL SIGTERM
VOLUME /etc/zrepl
WORKDIR "/tmp"
