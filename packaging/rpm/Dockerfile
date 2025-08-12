FROM almalinux:8

RUN dnf install -y git make bash rpm-build 'dnf-command(builddep)'
ADD packaging/rpm/zrepl.spec /tmp/zrepl.spec
RUN dnf builddep -y /tmp/zrepl.spec
RUN mkdir -p /build/src && chmod -R 0777 /build
WORKDIR /build/src
